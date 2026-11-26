# TripGo — лабораторная работа 1

HTTP-сервис создания, просмотра и завершения поездок. Данные хранятся в PostgreSQL. Выполнена обязательная часть лабораторной 1; идемпотентность и Dockerfile не реализованы.

## Требования

Работа ведётся в Ubuntu/WSL 2: Go 1.27.1, Git, GNU Make, запущенный Docker Desktop с интеграцией Ubuntu и установленный tripgoctl. Для `make test` с детектором гонок нужен C-компилятор (`gcc`, пакет `build-essential`). Инструменты oapi-codegen, goose и mockgen закреплены в go.mod.

## Запуск из чистого клона

```bash
git clone git@github.com:reybug/TripGo.git
cd TripGo
git switch homework/01
tripgoctl cluster start
tripgoctl environment start
tripgoctl connect
make migrate
make run
```

`tripgoctl environment start` создаёт `.env` с настоящим `DATABASE_URL`. Make читает настройки из `.env.example`, затем значения из `.env`, и передаёт их процессу как переменные окружения.

Сервис выполняет Ping базы при старте. Если база недоступна или конфигурация неверна, процесс завершается с ошибкой.

## Переменные окружения

Все переменные таблицы обязательны для сервиса. Значения, кроме DATABASE_URL, поставляет `.env.example` через Makefile. Адрес базы поставляет tripgoctl. При запуске напрямую через `go run ./cmd/trip-service` переменные нужно предварительно экспортировать самостоятельно.

| Переменная | Значение в примере | Назначение |
|---|---|---|
| HTTP_ADDR | :8080 | Адрес HTTP-сервера |
| LOG_LEVEL | info | Уровень логов: debug, info, warn, error |
| SHUTDOWN_TIMEOUT | 10s | Время ожидания завершения запросов при остановке |
| HTTP_READ_TIMEOUT | 5s | Чтение запроса (полностью) |
| HTTP_READ_HEADER_TIMEOUT | 2s | Чтение заголовков |
| HTTP_WRITE_TIMEOUT | 10s | Запись ответа |
| HTTP_IDLE_TIMEOUT | 60s | Ожидание следующего запроса в соединении |
| DATABASE_URL | создаёт tripgoctl | Строка подключения к PostgreSQL |
| DATABASE_MAX_CONNS | 10 | Максимальное число соединений |
| DATABASE_MIN_CONNS | 2 | Минимальное число соединений |
| DATABASE_MAX_CONN_LIFETIME | 30m | Срок жизни соединения |
| DATABASE_CONNECT_TIMEOUT | 5s | Подключение к базе и стартовый Ping |
| DATABASE_QUERY_TIMEOUT | 3s | Бюджет запроса к базе и транзакции целиком |

Таймауты должны быть положительными. Для пула требуется 0 <= min <= max, причём max > 0.

## Команды

```bash
make generate        # повторная генерация API
make build           # bin/trip-service
make migrate         # накатить обе миграции
make migrate-status  # состояние миграций
make migrate-down    # откатить последнюю миграцию
make run             # собрать и запустить
make test            # go test -race ./...
make lint            # go vet ./... для лабы 1
```

## Проверка HTTP

Для значения `:8080` из примера:

```bash
export BASE_URL=http://localhost:8080
curl -i "$BASE_URL/health"
curl -i "$BASE_URL/ready"
```

Создание поездки:

```bash
curl -i -X POST "$BASE_URL/api/v1/trips" \
  -H 'Content-Type: application/json' \
  -d '{"user_id":"5cb72c04-7650-45c9-a79b-bcdba0631e0c","driver_id":"8860b315-ec86-42eb-a17c-7c163d721ff5","start_point":{"latitude":59.9398,"longitude":30.3146},"end_point":{"latitude":59.9290,"longitude":30.3626},"price":1450}'
```

Ожидается `201`, поездка со статусом `active` и заголовок Location

```bash
export TRIP_ID=сюда_UUID_из_ответа
curl -i "$BASE_URL/api/v1/trips/$TRIP_ID"
curl -i -X POST "$BASE_URL/api/v1/trips/$TRIP_ID/finish"
curl -i -X POST "$BASE_URL/api/v1/trips/$TRIP_ID/finish"
```

Ответы по порядку: `200`, `200` со статусом `completed`, `409` с `code=trip_completed`. Повторное создание на того же водителя до завершения первой поездки даёт `409 driver_busy`. После завершения создание снова разрешено

Некорректный UUID, отрицательная цена, пропущенное обязательное поле, лишнее поле, null и неверный диапазон координат дают `400 invalid_request`. Неизвестный корректный UUID даёт `404 trip_not_found`. Ошибки поездок имеют Content-Type `application/problem+json`, а внутренние сообщения PostgreSQL клиенту не передаются.

`/health` возвращает `{"status":"ok"}` без обращения к базе. `/ready` выполняет Ping с таймаутом: `200 {"status":"ok"}` или `503 {"status":"unavailable"}`.

Для проверки параллельных созданий выполните:

```bash
seq 20 | xargs -P20 -I{} curl -s -o /dev/null -w '%{http_code}\n' \
  -X POST "$BASE_URL/api/v1/trips" -H 'Content-Type: application/json' \
  -d @trip.json | sort | uniq -c
```

Ожидается один `201` и девятнадцать `409`. Аналогично для ещё активной поездки:

```bash
seq 20 | xargs -P20 -I{} curl -s -o /dev/null -w '%{http_code}\n' \
  -X POST "$BASE_URL/api/v1/trips/$TRIP_ID/finish" | sort | uniq -c
```

Ожидается один `200` и девятнадцать `409`. Для проверки graceful shutdown остановите сервис через Ctrl+C: в логах должны появиться `shutdown started` и `shutdown completed`. SIGTERM обрабатывается так же

## Решения

- Транзакции используют *Read Committed*: для этой лабы хватит, потому что запрет двух активных поездок защищён уникальным индексом, а завершение - блокировкой строки.
- `Manager.Do` открывает транзакцию и кладёт её в context под закрытым типом ключа. Репо берёт из context транзакцию, а вне Do использует пул. Вложенный Do вызывает функцию с той же транзакцией. При успехе выполняется COMMIT, при ошибке или панике - ROLLBACK. Паника передаётся дальше. Для отката используется отдельный, ограниченный по времени, контекст, чтобы отмена исходного запроса не мешала очистки
- В бизнес-файле `service.go` нет pgx и SQL. Создание вызывает запись поездки и истории внутри одного Do. Завершение тоже меняет обе таблицы одной транзакцией
- Частичный уникальный индекс `trips_active_driver_idx` разрешает водителю сколько угодно завершённых поездок, но только одну active. Репо распознаёт ошибку PostgreSQL `23505` этого индекса как `driver_busy`
- При завершении `SELECT ... FOR UPDATE` держит блокировку строки до окончания транзакции. Второй запрос ждёт первый, затем видит completed и получает `trip_completed`. Время завершения не перезаписывается
- Таймаут остановки ограничивает ожидание активных запросов. При его превышении процесс пишет ошибку и завершается принудительно. При правильной остановке сначала завершается HTTP, затем закрывается пул.

## Файлы

- `cmd/trip-service/main.go` — запуск, подключение базы, остановка
- `internal/config/config.go` — чтение и проверка переменных
- `internal/trip/http.go` — HTTP и преобразование ошибок в ответы
- `internal/trip/service.go` — действия с поездкой
- `internal/trip/repository.go` — запросы PostgreSQL
- `internal/trip/transaction.go` — менеджер транзакций
- `internal/generated/api.gen.go` — результат генерации, вручную не редактируется
- `migrations/` — две миграции с up и down
- `contracts/` — контракты курса
