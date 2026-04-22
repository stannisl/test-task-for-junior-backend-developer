package task

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	taskdomain "example.com/taskservice/internal/domain/task"
)

type Repository interface {
	WithTx(ctx context.Context, fn func(repo Repository) error) error
	Create(ctx context.Context, task *taskdomain.Task) (*taskdomain.Task, error)
	CreateRecurrence(ctx context.Context, recurrence *taskdomain.Recurrence) (*taskdomain.Recurrence, error)
	ListDueRecurrences(ctx context.Context, dueBefore time.Time, limit int) ([]taskdomain.Recurrence, error)
	CreateSpawnedTask(ctx context.Context, recurrence *taskdomain.Recurrence, scheduledFor time.Time) (*taskdomain.Task, error)
	SetRecurrenceState(ctx context.Context, recurrenceID int64, lastSpawnedFor time.Time, nextSpawnAt time.Time, isActive bool, updatedAt time.Time) error
	GetByID(ctx context.Context, id int64) (*taskdomain.Task, error)
	Update(ctx context.Context, task *taskdomain.Task) (*taskdomain.Task, error)
	Delete(ctx context.Context, id int64) error
	List(ctx context.Context, input taskdomain.ListInput) ([]taskdomain.Task, error)
}

type Service struct {
	repo   Repository
	logger *slog.Logger
	now    func() time.Time
}

const (
	maxTitleLen      = 255
	maxCatchUpSpawns = 180
)

func NewService(repo Repository, logger *slog.Logger) *Service {
	return &Service{
		repo:   repo,
		logger: logger,
		now:    func() time.Time { return time.Now().UTC() },
	}
}

func (s *Service) Create(ctx context.Context, input CreateInput) (*taskdomain.Task, error) {
	now := s.now()
	normalized, err := validateCreateInput(input, now)
	if err != nil {
		return nil, err
	}

	if normalized.Repetition != "" {
		firstScheduledFor, ok, err := firstOccurrenceOnOrAfter(
			normalized.Repetition,
			normalized.Config,
			taskdomain.DateOnlyUTC(now),
		)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, fmt.Errorf(
				"%w: не найдено подходящих дат под настройки повторения задачи",
				ErrInvalidInput,
			)
		}

		var created *taskdomain.Task
		err = s.repo.WithTx(ctx, func(tx Repository) error {
			recurrence, err := tx.CreateRecurrence(ctx, &taskdomain.Recurrence{
				Title:            normalized.Title,
				Description:      normalized.Description,
				Repetition:       normalized.Repetition,
				RepetitionConfig: normalized.Config,
				IsActive:         true,
				NextSpawnAt:      firstScheduledFor,
			})
			if err != nil {
				return err
			}

			created, err = tx.CreateSpawnedTask(ctx, recurrence, firstScheduledFor)
			if err != nil {
				return err
			}

			nextSpawnAt, hasNext, err := nextOccurrenceAfter(
				normalized.Repetition,
				normalized.Config,
				firstScheduledFor,
			)
			if err != nil {
				return err
			}
			if !hasNext {
				nextSpawnAt = firstScheduledFor
			}

			return tx.SetRecurrenceState(ctx, recurrence.ID, firstScheduledFor, nextSpawnAt, hasNext, now)
		})
		if err != nil {
			return nil, err
		}

		return created, nil
	}

	model := &taskdomain.Task{
		Title:       normalized.Title,
		Description: normalized.Description,
		Status:      normalized.Status,
	}
	model.CreatedAt = now
	model.UpdatedAt = now

	created, err := s.repo.Create(ctx, model)
	if err != nil {
		return nil, err
	}

	return created, nil
}

func (s *Service) GetByID(ctx context.Context, id int64) (*taskdomain.Task, error) {
	if id <= 0 {
		return nil, fmt.Errorf("%w: id не может быть отрицательным", ErrInvalidInput)
	}

	return s.repo.GetByID(ctx, id)
}

func (s *Service) Update(ctx context.Context, id int64, input UpdateInput) (*taskdomain.Task, error) {
	if id <= 0 {
		return nil, fmt.Errorf("%w: id не может быть отрицательным", ErrInvalidInput)
	}

	normalized, err := validateUpdateInput(input)
	if err != nil {
		return nil, err
	}
	if normalized.Repetition != "" || len(normalized.Config) > 0 {
		return nil, fmt.Errorf(
			"%w: настройки повторения могут быть установлены только при создании задачи",
			ErrInvalidInput,
		)
	}

	model := &taskdomain.Task{
		ID:          id,
		Title:       normalized.Title,
		Description: normalized.Description,
		Status:      normalized.Status,
		UpdatedAt:   s.now(),
	}

	updated, err := s.repo.Update(ctx, model)
	if err != nil {
		return nil, err
	}

	return updated, nil
}

func (s *Service) Delete(ctx context.Context, id int64) error {
	if id <= 0 {
		return fmt.Errorf("%w: id не может быть отрицательным", ErrInvalidInput)
	}

	return s.repo.Delete(ctx, id)
}

func (s *Service) List(ctx context.Context, input taskdomain.ListInput) ([]taskdomain.Task, error) {
	normalized := validateListInput(input)
	return s.repo.List(ctx, normalized)
}

func (s *Service) SpawnDueRecurrences(ctx context.Context, limit int) (int, error) {
	if limit <= 0 {
		limit = 100
	}

	now := s.now()
	due, err := s.repo.ListDueRecurrences(ctx, now, limit)
	if err != nil {
		return 0, err
	}

	today := taskdomain.DateOnlyUTC(now)
	createdCount := 0

	for i := range due {
		recurrence := due[i]
		spawnDates, nextSpawnAt, isActive, capped, err := planSpawnDates(recurrence, today)
		if err != nil {
			return createdCount, err
		}
		if capped {
			s.logger.Warn(
				"recurrence достиг spawn предела, даты могут быть пропущены",
				"recurrence_id", recurrence.ID,
				"cap", maxCatchUpSpawns,
			)
		}
		if len(spawnDates) == 0 {
			continue
		}

		localCount := 0
		err = s.repo.WithTx(ctx, func(tx Repository) error {
			for _, scheduledFor := range spawnDates {
				if _, err := tx.CreateSpawnedTask(ctx, &recurrence, scheduledFor); err != nil {
					return err
				}
				localCount++
			}

			lastSpawnedFor := spawnDates[len(spawnDates)-1]
			return tx.SetRecurrenceState(ctx, recurrence.ID, lastSpawnedFor, nextSpawnAt, isActive, now)
		})
		if err != nil {
			return createdCount, err
		}

		createdCount += localCount
	}

	return createdCount, nil
}

func validateCreateInput(input CreateInput, now time.Time) (CreateInput, error) {
	input.Title = strings.TrimSpace(input.Title)
	input.Description = strings.TrimSpace(input.Description)

	if input.Title == "" {
		return CreateInput{}, fmt.Errorf("%w: title обязателен", ErrInvalidInput)
	}
	if len([]rune(input.Title)) > maxTitleLen {
		return CreateInput{}, fmt.Errorf("%w: title не должен превышать %d символов", ErrInvalidInput, maxTitleLen)
	}

	if input.Status == "" {
		input.Status = taskdomain.StatusNew
	}

	if !input.Status.Valid() {
		return CreateInput{}, fmt.Errorf("%w: неверный status", ErrInvalidInput)
	}

	input.Repetition = taskdomain.Repetition(strings.TrimSpace(string(input.Repetition)))
	if input.Repetition == "" {
		if len(input.Config) > 0 {
			return CreateInput{}, fmt.Errorf(
				"%w: repetition_type должен быть, если repetition_config был в запросе",
				ErrInvalidInput,
			)
		}
		return input, nil
	}

	if !input.Repetition.Valid() {
		return CreateInput{}, fmt.Errorf("%w: неверный repetition_type", ErrInvalidInput)
	}

	if input.Status != taskdomain.StatusNew {
		return CreateInput{}, fmt.Errorf(
			"%w: повторяющаяся задача может быть создана только с status=New",
			ErrInvalidInput,
		)
	}

	normalizedConfig, err := normalizeRecurrenceConfig(input.Repetition, input.Config, taskdomain.DateOnlyUTC(now))
	if err != nil {
		return CreateInput{}, err
	}
	input.Config = normalizedConfig

	return input, nil
}

func validateUpdateInput(input UpdateInput) (UpdateInput, error) {
	input.Title = strings.TrimSpace(input.Title)
	input.Description = strings.TrimSpace(input.Description)

	if input.Title == "" {
		return UpdateInput{}, fmt.Errorf("%w: title обязателен", ErrInvalidInput)
	}
	if len([]rune(input.Title)) > maxTitleLen {
		return UpdateInput{}, fmt.Errorf("%w: title не должен превышать %d символов", ErrInvalidInput, maxTitleLen)
	}

	if !input.Status.Valid() {
		return UpdateInput{}, fmt.Errorf("%w: неверный status", ErrInvalidInput)
	}

	input.Repetition = taskdomain.Repetition(strings.TrimSpace(string(input.Repetition)))
	if input.Repetition == "" {
		if len(input.Config) > 0 {
			return UpdateInput{}, fmt.Errorf(
				"%w: repetition_type должен быть, если repetition_config добавлен", ErrInvalidInput)
		}
		return input, nil
	}

	if !input.Repetition.Valid() {
		return UpdateInput{}, fmt.Errorf("%w: неверный repetition_type", ErrInvalidInput)
	}

	return input, nil
}

func validateListInput(input taskdomain.ListInput) taskdomain.ListInput {
	if input.Limit <= 0 {
		input.Limit = 50
	}
	if input.Limit > 200 {
		input.Limit = 200
	}
	if input.Offset < 0 {
		input.Offset = 0
	}

	return input
}

type dailyConfig struct {
	IntervalDays int `json:"interval_days"`
}

type monthDaysConfig struct {
	Days []int `json:"days"`
}

type specificDatesConfig struct {
	Dates []string `json:"dates"`
}

func normalizeRecurrenceConfig(repetition taskdomain.Repetition, raw json.RawMessage, today time.Time) (json.RawMessage, error) {
	switch repetition {
	case taskdomain.RepetitionTypeDaily:
		cfg := dailyConfig{IntervalDays: 1}
		if len(raw) > 0 {
			if err := json.Unmarshal(raw, &cfg); err != nil {
				return nil, fmt.Errorf("%w: неверный daily repetition_config", ErrInvalidInput)
			}
		}
		if cfg.IntervalDays <= 0 {
			return nil, fmt.Errorf("%w: interval_days должен быть больше 0", ErrInvalidInput)
		}

		return json.Marshal(cfg)
	case taskdomain.RepetitionTypeMonthDays:
		cfg := monthDaysConfig{}
		if err := json.Unmarshal(raw, &cfg); err != nil {
			return nil, fmt.Errorf("%w: неверный month_days repetition_config", ErrInvalidInput)
		}
		if len(cfg.Days) == 0 {
			return nil, fmt.Errorf("%w: month_days требует хотя один день", ErrInvalidInput)
		}

		daysSet := make(map[int]struct{}, len(cfg.Days))
		normalizedDays := make([]int, 0, len(cfg.Days))
		for _, day := range cfg.Days {
			if day < 1 || day > 30 {
				return nil, fmt.Errorf("%w: month_days должен быть в диапазоне [1..30]", ErrInvalidInput)
			}
			if _, exists := daysSet[day]; exists {
				continue
			}
			daysSet[day] = struct{}{}
			normalizedDays = append(normalizedDays, day)
		}
		sort.Ints(normalizedDays)

		return json.Marshal(monthDaysConfig{Days: normalizedDays})
	case taskdomain.RepetitionTypeSpecificDates:
		cfg := specificDatesConfig{}
		if err := json.Unmarshal(raw, &cfg); err != nil {
			return nil, fmt.Errorf("%w: неверный specific_dates repetition_config", ErrInvalidInput)
		}
		if len(cfg.Dates) == 0 {
			return nil, fmt.Errorf("%w: specific_dates требует хотя бы одну дату", ErrInvalidInput)
		}

		parsedDates := make([]time.Time, 0, len(cfg.Dates))
		unique := make(map[string]struct{}, len(cfg.Dates))
		hasFuture := false
		for _, rawDate := range cfg.Dates {
			date, err := time.Parse("2006-01-02", rawDate)
			if err != nil {
				return nil, fmt.Errorf("%w: specific_dates должен быть YYYY-MM-DD формата", ErrInvalidInput)
			}
			date = taskdomain.DateOnlyUTC(date)
			key := date.Format("2006-01-02")
			if _, exists := unique[key]; exists {
				continue
			}
			unique[key] = struct{}{}
			parsedDates = append(parsedDates, date)
			if !date.Before(today) {
				hasFuture = true
			}
		}

		if !hasFuture {
			return nil, fmt.Errorf(
				"%w: specific_dates должен содержать хотя бы одну дату в будущем или настоящем",
				ErrInvalidInput)
		}

		sort.Slice(parsedDates, func(i, j int) bool {
			return parsedDates[i].Before(parsedDates[j])
		})

		normalizedDates := make([]string, 0, len(parsedDates))
		for _, date := range parsedDates {
			normalizedDates = append(normalizedDates, date.Format("2006-01-02"))
		}

		return json.Marshal(specificDatesConfig{Dates: normalizedDates})
	case taskdomain.RepetitionTypeEvenMonth, taskdomain.RepetitionTypeOddMonth:
		if len(raw) == 0 {
			return json.RawMessage("{}"), nil
		}

		probe := map[string]any{}
		if err := json.Unmarshal(raw, &probe); err != nil {
			return nil, fmt.Errorf("%w: invalid repetition_config", ErrInvalidInput)
		}
		if len(probe) > 0 {
			return nil, fmt.Errorf(
				"%w: repetition_config is not allowed for even_month/odd_month",
				ErrInvalidInput,
			)
		}

		return json.RawMessage("{}"), nil
	default:
		return nil, fmt.Errorf("%w: invalid repetition_type", ErrInvalidInput)
	}
}

func firstOccurrenceOnOrAfter(repetition taskdomain.Repetition, raw json.RawMessage, fromDate time.Time) (time.Time, bool, error) {
	fromDate = taskdomain.DateOnlyUTC(fromDate)

	switch repetition {
	case taskdomain.RepetitionTypeDaily:
		return fromDate, true, nil
	case taskdomain.RepetitionTypeEvenMonth:
		if fromDate.Day()%2 == 0 {
			return fromDate, true, nil
		}
		return fromDate.AddDate(0, 0, 1), true, nil
	case taskdomain.RepetitionTypeOddMonth:
		if fromDate.Day()%2 == 1 {
			return fromDate, true, nil
		}
		return fromDate.AddDate(0, 0, 1), true, nil
	case taskdomain.RepetitionTypeMonthDays:
		cfg := monthDaysConfig{}
		if err := json.Unmarshal(raw, &cfg); err != nil {
			return time.Time{}, false, fmt.Errorf("%w: invalid month_days repetition_config", ErrInvalidInput)
		}

		candidate := fromDate
		for i := 0; i < 36; i++ {
			year, month, _ := candidate.Date()
			monthDaysCount := daysInMonth(year, month)
			for _, day := range cfg.Days {
				if day > monthDaysCount {
					continue
				}

				occurrence := time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
				if !occurrence.Before(fromDate) {
					return occurrence, true, nil
				}
			}

			candidate = time.Date(year, month, 1, 0, 0, 0, 0, time.UTC).
				AddDate(0, 1, 0)
		}

		return time.Time{}, false, fmt.Errorf("%w: failed to calculate next month_days occurrence", ErrInvalidInput)
	case taskdomain.RepetitionTypeSpecificDates:
		cfg := specificDatesConfig{}
		if err := json.Unmarshal(raw, &cfg); err != nil {
			return time.Time{}, false, fmt.Errorf("%w: invalid specific_dates repetition_config", ErrInvalidInput)
		}

		for _, dateStr := range cfg.Dates {
			date, err := time.Parse("2006-01-02", dateStr)
			if err != nil {
				return time.Time{}, false, fmt.Errorf("%w: specific_dates must use YYYY-MM-DD format", ErrInvalidInput)
			}
			date = taskdomain.DateOnlyUTC(date)
			if !date.Before(fromDate) {
				return date, true, nil
			}
		}

		return time.Time{}, false, nil
	default:
		return time.Time{}, false, fmt.Errorf("%w: invalid repetition_type", ErrInvalidInput)
	}
}

func nextOccurrenceAfter(repetition taskdomain.Repetition, raw json.RawMessage, afterDate time.Time) (time.Time, bool, error) {
	afterDate = taskdomain.DateOnlyUTC(afterDate)

	switch repetition {
	case taskdomain.RepetitionTypeDaily:
		cfg := dailyConfig{}
		if err := json.Unmarshal(raw, &cfg); err != nil {
			return time.Time{}, false, fmt.Errorf("%w: invalid daily repetition_config", ErrInvalidInput)
		}
		if cfg.IntervalDays <= 0 {
			return time.Time{}, false, fmt.Errorf("%w: interval_days must be greater than zero", ErrInvalidInput)
		}

		return afterDate.AddDate(0, 0, cfg.IntervalDays), true, nil
	case taskdomain.RepetitionTypeMonthDays,
		taskdomain.RepetitionTypeEvenMonth,
		taskdomain.RepetitionTypeOddMonth,
		taskdomain.RepetitionTypeSpecificDates:
		return firstOccurrenceOnOrAfter(repetition, raw, afterDate.AddDate(0, 0, 1))
	default:
		return time.Time{}, false, fmt.Errorf("%w: invalid repetition_type", ErrInvalidInput)
	}
}

func planSpawnDates(recurrence taskdomain.Recurrence, today time.Time) ([]time.Time, time.Time, bool, bool, error) {
	today = taskdomain.DateOnlyUTC(today)
	next := taskdomain.DateOnlyUTC(recurrence.NextSpawnAt)
	if next.After(today) {
		return nil, next, recurrence.IsActive, false, nil
	}

	spawnDates := make([]time.Time, 0, 4)
	current := next
	for i := 0; i < maxCatchUpSpawns && !current.After(today); i++ {
		spawnDates = append(spawnDates, current)

		nextDate, hasNext, err := nextOccurrenceAfter(recurrence.Repetition, recurrence.RepetitionConfig, current)
		if err != nil {
			return nil, time.Time{}, false, false, err
		}
		if !hasNext {
			return spawnDates, current, false, false, nil
		}

		current = nextDate
	}

	capped := len(spawnDates) == maxCatchUpSpawns && !current.After(today)

	if len(spawnDates) == 0 {
		return nil, current, true, capped, nil
	}

	return spawnDates, current, true, capped, nil
}

func daysInMonth(year int, month time.Month) int {
	return time.Date(year, month+1, 0, 0, 0, 0, 0, time.UTC).Day()
}
