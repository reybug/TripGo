SHELL := /bin/sh

include .env.example
-include .env
export

.PHONY: generate build run migrate migrate-down migrate-status test lint

generate:
	go tool oapi-codegen -generate types,chi-server,spec -include-operation-ids createTrip,getTrip,finishTrip,health,ready -package api -o internal/generated/api.gen.go contracts/openapi/trip-service.openapi.yaml

build:
	go build -o bin/trip-service ./cmd/trip-service

run: build
	./bin/trip-service

migrate:
	go tool goose -dir migrations postgres "$(DATABASE_URL)" up

migrate-down:
	go tool goose -dir migrations postgres "$(DATABASE_URL)" down

migrate-status:
	go tool goose -dir migrations postgres "$(DATABASE_URL)" status

test:
	go test -race ./...

lint:
	go vet ./...
