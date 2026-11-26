package trip

import (
	"context"
	"errors"
	"fmt"
	"time"

	sq "github.com/Masterminds/squirrel"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	api "github.com/reybug/TripGo/internal/generated"
)

type Repository struct {
	pool    *pgxpool.Pool
	timeout time.Duration
}

func NewRepository(pool *pgxpool.Pool, timeout time.Duration) *Repository {
	return &Repository{pool: pool, timeout: timeout}
}

func builder() sq.StatementBuilderType { return sq.StatementBuilder.PlaceholderFormat(sq.Dollar) }

func (r *Repository) Insert(ctx context.Context, t api.Trip) error {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	query, args, err := builder().Insert("trips").Columns("id", "user_id", "driver_id", "start_latitude", "start_longitude", "end_latitude", "end_longitude", "price", "status", "started_at").Values(t.Id, t.UserId, t.DriverId, t.StartPoint.Latitude, t.StartPoint.Longitude, t.EndPoint.Latitude, t.EndPoint.Longitude, t.Price, t.Status, t.StartedAt).ToSql()
	if err != nil {
		return fmt.Errorf("build insert trip: %w", err)
	}
	_, err = r.executor(ctx).Exec(ctx, query, args...)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == "trips_active_driver_idx" {
		return ErrDriverBusy
	}
	if err != nil {
		return fmt.Errorf("insert trip: %w", err)
	}
	return nil
}

func (r *Repository) Get(ctx context.Context, id uuid.UUID, lock bool) (api.Trip, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	q := builder().Select("id", "user_id", "driver_id", "start_latitude", "start_longitude", "end_latitude", "end_longitude", "price", "status", "started_at", "finished_at").From("trips").Where(sq.Eq{"id": id})
	if lock {
		q = q.Suffix("FOR UPDATE")
	}
	query, args, err := q.ToSql()
	if err != nil {
		return api.Trip{}, fmt.Errorf("build get trip: %w", err)
	}
	var t api.Trip
	err = r.executor(ctx).QueryRow(ctx, query, args...).Scan(&t.Id, &t.UserId, &t.DriverId, &t.StartPoint.Latitude, &t.StartPoint.Longitude, &t.EndPoint.Latitude, &t.EndPoint.Longitude, &t.Price, &t.Status, &t.StartedAt, &t.FinishedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return t, ErrNotFound
	}
	if err != nil {
		return t, fmt.Errorf("get trip: %w", err)
	}
	t.StartedAt = t.StartedAt.UTC()
	if t.FinishedAt != nil {
		v := t.FinishedAt.UTC()
		t.FinishedAt = &v
	}
	return t, nil
}

func (r *Repository) Finish(ctx context.Context, t api.Trip) error {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	query, args, err := builder().Update("trips").Set("status", t.Status).Set("finished_at", t.FinishedAt).Set("updated_at", t.FinishedAt).Where(sq.Eq{"id": t.Id}).ToSql()
	if err != nil {
		return fmt.Errorf("build finish trip: %w", err)
	}
	if _, err = r.executor(ctx).Exec(ctx, query, args...); err != nil {
		return fmt.Errorf("finish trip: %w", err)
	}
	return nil
}

func (r *Repository) History(ctx context.Context, id uuid.UUID, from *api.TripStatus, to api.TripStatus, at time.Time) error {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	query, args, err := builder().Insert("trip_status_history").Columns("trip_id", "from_status", "to_status", "changed_at").Values(id, from, to, at).ToSql()
	if err != nil {
		return fmt.Errorf("build insert history: %w", err)
	}
	if _, err = r.executor(ctx).Exec(ctx, query, args...); err != nil {
		return fmt.Errorf("insert history: %w", err)
	}
	return nil
}
