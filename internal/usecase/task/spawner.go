package task

import (
	"context"
	"log/slog"
	"time"

	taskdomain "example.com/taskservice/internal/domain/task"
)

type Usecase interface {
	Create(ctx context.Context, input CreateInput) (*taskdomain.Task, error)
	GetByID(ctx context.Context, id int64) (*taskdomain.Task, error)
	Update(ctx context.Context, id int64, input UpdateInput) (*taskdomain.Task, error)
	Delete(ctx context.Context, id int64) error
	List(ctx context.Context, input taskdomain.ListInput) ([]taskdomain.Task, error)
	SpawnDueRecurrences(ctx context.Context, limit int) (int, error)
}

type Spawner struct {
	usecase  Usecase
	logger   *slog.Logger
	interval time.Duration
	batch    int
}

func NewSpawner(usecase Usecase, logger *slog.Logger, interval time.Duration, batch int) *Spawner {
	if interval <= 0 {
		interval = time.Minute
	}
	if batch <= 0 {
		batch = 100
	}

	return &Spawner{
		usecase:  usecase,
		logger:   logger,
		interval: interval,
		batch:    batch,
	}
}

func (s *Spawner) Run(ctx context.Context) {
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	s.spawnOnce(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.spawnOnce(ctx)
		}
	}
}

func (s *Spawner) spawnOnce(ctx context.Context) {
	created, err := s.usecase.SpawnDueRecurrences(ctx, s.batch)
	if err != nil {
		s.logger.Error("spawn recurring tasks", "error", err)
		return
	}

	if created > 0 {
		s.logger.Info("spawned recurring tasks", "created", created)
	}
}
