package trip

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	api "github.com/reybug/TripGo/internal/generated"
)

var (
	ErrNotFound   = errors.New("trip not found")
	ErrCompleted  = errors.New("trip completed")
	ErrDriverBusy = errors.New("driver busy")
)

type Service struct {
	repo *Repository
	tx   TxManager
}

func NewService(repo *Repository, tx TxManager) *Service {
	return &Service{repo: repo, tx: tx}
}

func (s *Service) Create(ctx context.Context, data api.TripData) (api.Trip, error) {
	t := api.Trip{Id: uuid.New(), UserId: data.UserId, DriverId: data.DriverId,
		StartPoint: data.StartPoint, EndPoint: data.EndPoint, Price: data.Price,
		Status: api.Active, StartedAt: time.Now().UTC().Truncate(time.Microsecond)}
	err := s.tx.Do(ctx, func(ctx context.Context) error {
		if err := s.repo.Insert(ctx, t); err != nil {
			return err
		}
		return s.repo.History(ctx, t.Id, nil, api.Active, t.StartedAt)
	})
	return t, err
}

func (s *Service) Get(ctx context.Context, id uuid.UUID) (api.Trip, error) {
	return s.repo.Get(ctx, id, false)
}

func (s *Service) Finish(ctx context.Context, id uuid.UUID) (api.Trip, error) {
	var t api.Trip
	err := s.tx.Do(ctx, func(ctx context.Context) error {
		var err error
		t, err = s.repo.Get(ctx, id, true)
		if err != nil {
			return err
		}
		if t.Status == api.Completed {
			return ErrCompleted
		}
		now := time.Now().UTC().Truncate(time.Microsecond)
		t.Status, t.FinishedAt = api.Completed, &now
		if err := s.repo.Finish(ctx, t); err != nil {
			return err
		}
		old := api.Active
		return s.repo.History(ctx, id, &old, api.Completed, now)
	})
	return t, err
}
