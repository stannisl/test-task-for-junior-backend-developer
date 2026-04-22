package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	taskusecase "example.com/taskservice/internal/usecase/task"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	taskdomain "example.com/taskservice/internal/domain/task"
)

type dbExecutor interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

type TaskRepository struct {
	pool *pgxpool.Pool
}

type txRepository struct {
	tx pgx.Tx
}

func New(pool *pgxpool.Pool) *TaskRepository {
	return &TaskRepository{pool: pool}
}

func (r *TaskRepository) WithTx(ctx context.Context, fn func(repo taskusecase.Repository) error) (err error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}

	txRepo := &txRepository{tx: tx}

	defer func() {
		if recovered := recover(); recovered != nil {
			_ = tx.Rollback(ctx)
			panic(recovered)
		}

		if err != nil {
			_ = tx.Rollback(ctx)
			return
		}

		err = tx.Commit(ctx)
	}()

	err = fn(txRepo)
	return err
}

func (r *txRepository) WithTx(ctx context.Context, fn func(repo taskusecase.Repository) error) error {
	return fn(r)
}

func (r *TaskRepository) Create(ctx context.Context, task *taskdomain.Task) (*taskdomain.Task, error) {
	return createTask(ctx, r.pool, task)
}

func (r *txRepository) Create(ctx context.Context, task *taskdomain.Task) (*taskdomain.Task, error) {
	return createTask(ctx, r.tx, task)
}

func createTask(ctx context.Context, db dbExecutor, task *taskdomain.Task) (*taskdomain.Task, error) {
	const query = `
		WITH inserted AS (
			INSERT INTO tasks (title, description, status, created_at, updated_at, recurrence_id, scheduled_for)
			VALUES ($1, $2, $3, $4, $5, $6, $7)
			RETURNING id, title, description, status, created_at, updated_at, recurrence_id, scheduled_for
		)
		SELECT i.id,
			i.title,
			i.description,
			i.status,
			i.created_at,
			i.updated_at,
			i.recurrence_id,
			i.scheduled_for,
			r.recurrence_type,
			r.recurrence_config
		FROM inserted i
		LEFT JOIN task_recurrences r ON r.id = i.recurrence_id
	`

	row := db.QueryRow(
		ctx,
		query,
		task.Title,
		task.Description,
		task.Status,
		task.CreatedAt,
		task.UpdatedAt,
		task.RecurrenceID,
		task.ScheduledFor,
	)
	created, err := scanTask(row)
	if err != nil {
		return nil, err
	}

	return created, nil
}

func (r *TaskRepository) CreateRecurrence(ctx context.Context, recurrence *taskdomain.Recurrence) (*taskdomain.Recurrence, error) {
	return createRecurrence(ctx, r.pool, recurrence)
}

func (r *txRepository) CreateRecurrence(ctx context.Context, recurrence *taskdomain.Recurrence) (*taskdomain.Recurrence, error) {
	return createRecurrence(ctx, r.tx, recurrence)
}

func createRecurrence(ctx context.Context, db dbExecutor, recurrence *taskdomain.Recurrence) (*taskdomain.Recurrence, error) {
	const query = `
		INSERT INTO task_recurrences (
			title,
			description,
			recurrence_type,
			recurrence_config,
			is_active,
			next_spawn_at,
			created_at,
			updated_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, NOW(), NOW())
		RETURNING id,
			title,
			description,
			recurrence_type,
			recurrence_config,
			is_active,
			last_spawned_for,
			next_spawn_at,
			created_at,
			updated_at
	`

	row := db.QueryRow(
		ctx,
		query,
		recurrence.Title,
		recurrence.Description,
		recurrence.Repetition,
		recurrence.RepetitionConfig,
		recurrence.IsActive,
		recurrence.NextSpawnAt,
	)

	return scanRecurrence(row)
}

func (r *TaskRepository) ListDueRecurrences(ctx context.Context, dueBefore time.Time, limit int) ([]taskdomain.Recurrence, error) {
	return listDueRecurrences(ctx, r.pool, dueBefore, limit)
}

func (r *txRepository) ListDueRecurrences(ctx context.Context, dueBefore time.Time, limit int) ([]taskdomain.Recurrence, error) {
	return listDueRecurrences(ctx, r.tx, dueBefore, limit)
}

func listDueRecurrences(ctx context.Context, db dbExecutor, dueBefore time.Time, limit int) ([]taskdomain.Recurrence, error) {
	const query = `
		SELECT id,
			title,
			description,
			recurrence_type,
			recurrence_config,
			is_active,
			last_spawned_for,
			next_spawn_at,
			created_at,
			updated_at
		FROM task_recurrences
		WHERE is_active = TRUE
			AND next_spawn_at <= $1
		ORDER BY next_spawn_at ASC, id ASC
		LIMIT $2
	`

	rows, err := db.Query(ctx, query, dueBefore, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	recurrences := make([]taskdomain.Recurrence, 0)
	for rows.Next() {
		recurrence, err := scanRecurrence(rows)
		if err != nil {
			return nil, err
		}

		recurrences = append(recurrences, *recurrence)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return recurrences, nil
}

func (r *TaskRepository) CreateSpawnedTask(ctx context.Context, recurrence *taskdomain.Recurrence, scheduledFor time.Time) (*taskdomain.Task, error) {
	return createSpawnedTask(ctx, r.pool, recurrence, scheduledFor)
}

func (r *txRepository) CreateSpawnedTask(ctx context.Context, recurrence *taskdomain.Recurrence, scheduledFor time.Time) (*taskdomain.Task, error) {
	return createSpawnedTask(ctx, r.tx, recurrence, scheduledFor)
}

func createSpawnedTask(ctx context.Context, db dbExecutor, recurrence *taskdomain.Recurrence, scheduledFor time.Time) (*taskdomain.Task, error) {
	const query = `
		WITH inserted AS (
			INSERT INTO tasks (
				title,
				description,
				status,
				created_at,
				updated_at,
				recurrence_id,
				scheduled_for
			)
			VALUES ($1, $2, $3, NOW(), NOW(), $4, $5)
			ON CONFLICT (recurrence_id, scheduled_for)
			WHERE recurrence_id IS NOT NULL AND scheduled_for IS NOT NULL
			DO NOTHING
			RETURNING id, title, description, status, created_at, updated_at, recurrence_id, scheduled_for
		)
		SELECT i.id,
			i.title,
			i.description,
			i.status,
			i.created_at,
			i.updated_at,
			i.recurrence_id,
			i.scheduled_for,
			r.recurrence_type,
			r.recurrence_config
		FROM inserted i
		LEFT JOIN task_recurrences r ON r.id = i.recurrence_id

		UNION ALL

		SELECT t.id,
			t.title,
			t.description,
			t.status,
			t.created_at,
			t.updated_at,
			t.recurrence_id,
			t.scheduled_for,
			r.recurrence_type,
			r.recurrence_config
		FROM tasks t
		LEFT JOIN task_recurrences r ON r.id = t.recurrence_id
		WHERE t.recurrence_id = $4
			AND t.scheduled_for = $5
			AND NOT EXISTS (SELECT 1 FROM inserted)
		LIMIT 1
	`

	row := db.QueryRow(
		ctx,
		query,
		recurrence.Title,
		recurrence.Description,
		taskdomain.StatusNew,
		recurrence.ID,
		taskdomain.DateOnlyUTC(scheduledFor),
	)

	created, err := scanTask(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, taskdomain.ErrNotFound
		}
		return nil, err
	}

	return created, nil
}

func (r *TaskRepository) SetRecurrenceState(ctx context.Context, recurrenceID int64, lastSpawnedFor time.Time, nextSpawnAt time.Time, isActive bool, updatedAt time.Time) error {
	return setRecurrenceState(ctx, r.pool, recurrenceID, lastSpawnedFor, nextSpawnAt, isActive, updatedAt)
}

func (r *txRepository) SetRecurrenceState(ctx context.Context, recurrenceID int64, lastSpawnedFor time.Time, nextSpawnAt time.Time, isActive bool, updatedAt time.Time) error {
	return setRecurrenceState(ctx, r.tx, recurrenceID, lastSpawnedFor, nextSpawnAt, isActive, updatedAt)
}

func setRecurrenceState(ctx context.Context, db dbExecutor, recurrenceID int64, lastSpawnedFor time.Time, nextSpawnAt time.Time, isActive bool, updatedAt time.Time) error {
	const query = `
		UPDATE task_recurrences
		SET last_spawned_for = $1,
			next_spawn_at = $2,
			is_active = $3,
			updated_at = $4
		WHERE id = $5
	`

	result, err := db.Exec(
		ctx,
		query,
		taskdomain.DateOnlyUTC(lastSpawnedFor),
		taskdomain.DateOnlyUTC(nextSpawnAt),
		isActive,
		updatedAt,
		recurrenceID,
	)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return taskdomain.ErrNotFound
	}

	return nil
}

func (r *TaskRepository) GetByID(ctx context.Context, id int64) (*taskdomain.Task, error) {
	return getTaskByID(ctx, r.pool, id)
}

func (r *txRepository) GetByID(ctx context.Context, id int64) (*taskdomain.Task, error) {
	return getTaskByID(ctx, r.tx, id)
}

func getTaskByID(ctx context.Context, db dbExecutor, id int64) (*taskdomain.Task, error) {
	const query = `
		SELECT t.id,
			t.title,
			t.description,
			t.status,
			t.created_at,
			t.updated_at,
			t.recurrence_id,
			t.scheduled_for,
			r.recurrence_type,
			r.recurrence_config
		FROM tasks t
		LEFT JOIN task_recurrences r ON r.id = t.recurrence_id
		WHERE t.id = $1
	`

	row := db.QueryRow(ctx, query, id)
	found, err := scanTask(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, taskdomain.ErrNotFound
		}

		return nil, err
	}

	return found, nil
}

func (r *TaskRepository) Update(ctx context.Context, task *taskdomain.Task) (*taskdomain.Task, error) {
	return updateTask(ctx, r.pool, task)
}

func (r *txRepository) Update(ctx context.Context, task *taskdomain.Task) (*taskdomain.Task, error) {
	return updateTask(ctx, r.tx, task)
}

func updateTask(ctx context.Context, db dbExecutor, task *taskdomain.Task) (*taskdomain.Task, error) {
	const query = `
		WITH updated AS (
			UPDATE tasks
			SET title = $1,
				description = $2,
				status = $3,
				updated_at = $4
			WHERE id = $5
			RETURNING id, title, description, status, created_at, updated_at, recurrence_id, scheduled_for
		)
		SELECT u.id,
			u.title,
			u.description,
			u.status,
			u.created_at,
			u.updated_at,
			u.recurrence_id,
			u.scheduled_for,
			r.recurrence_type,
			r.recurrence_config
		FROM updated u
		LEFT JOIN task_recurrences r ON r.id = u.recurrence_id
	`

	row := db.QueryRow(ctx, query, task.Title, task.Description, task.Status, task.UpdatedAt, task.ID)
	updated, err := scanTask(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, taskdomain.ErrNotFound
		}

		return nil, err
	}

	return updated, nil
}

func (r *TaskRepository) Delete(ctx context.Context, id int64) error {
	return deleteTask(ctx, r.pool, id)
}

func (r *txRepository) Delete(ctx context.Context, id int64) error {
	return deleteTask(ctx, r.tx, id)
}

func deleteTask(ctx context.Context, db dbExecutor, id int64) error {
	const query = `DELETE FROM tasks WHERE id = $1`

	result, err := db.Exec(ctx, query, id)
	if err != nil {
		return err
	}

	if result.RowsAffected() == 0 {
		return taskdomain.ErrNotFound
	}

	return nil
}

func (r *TaskRepository) List(ctx context.Context, input taskdomain.ListInput) ([]taskdomain.Task, error) {
	return listTasks(ctx, r.pool, input)
}

func (r *txRepository) List(ctx context.Context, input taskdomain.ListInput) ([]taskdomain.Task, error) {
	return listTasks(ctx, r.tx, input)
}

func listTasks(ctx context.Context, db dbExecutor, input taskdomain.ListInput) ([]taskdomain.Task, error) {
	const query = `
		SELECT t.id,
			t.title,
			t.description,
			t.status,
			t.created_at,
			t.updated_at,
			t.recurrence_id,
			t.scheduled_for,
			r.recurrence_type,
			r.recurrence_config
		FROM tasks t
		LEFT JOIN task_recurrences r ON r.id = t.recurrence_id
		ORDER BY t.id DESC
		LIMIT $1 OFFSET $2
	`

	rows, err := db.Query(ctx, query, input.Limit, input.Offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	tasks := make([]taskdomain.Task, 0)
	for rows.Next() {
		task, err := scanTask(rows)
		if err != nil {
			return nil, err
		}

		tasks = append(tasks, *task)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return tasks, nil
}

type taskScanner interface {
	Scan(dest ...any) error
}

func scanTask(scanner taskScanner) (*taskdomain.Task, error) {
	var (
		task             taskdomain.Task
		status           string
		recurrenceID     sql.NullInt64
		scheduledFor     sql.NullTime
		recurrenceType   sql.NullString
		recurrenceConfig []byte
	)

	if err := scanner.Scan(
		&task.ID,
		&task.Title,
		&task.Description,
		&status,
		&task.CreatedAt,
		&task.UpdatedAt,
		&recurrenceID,
		&scheduledFor,
		&recurrenceType,
		&recurrenceConfig,
	); err != nil {
		return nil, err
	}

	task.Status = taskdomain.Status(status)
	if recurrenceID.Valid {
		value := recurrenceID.Int64
		task.RecurrenceID = &value
	}
	if scheduledFor.Valid {
		value := taskdomain.DateOnlyUTC(scheduledFor.Time)
		task.ScheduledFor = &value
	}
	if recurrenceType.Valid {
		task.Repetition = taskdomain.Repetition(recurrenceType.String)
	}
	if len(recurrenceConfig) > 0 {
		task.RepetitionConfig = append(json.RawMessage(nil), recurrenceConfig...)
	}

	return &task, nil
}

func scanRecurrence(scanner taskScanner) (*taskdomain.Recurrence, error) {
	var (
		recurrence       taskdomain.Recurrence
		recurrenceType   string
		recurrenceConfig []byte
		lastSpawnedFor   sql.NullTime
	)

	if err := scanner.Scan(
		&recurrence.ID,
		&recurrence.Title,
		&recurrence.Description,
		&recurrenceType,
		&recurrenceConfig,
		&recurrence.IsActive,
		&lastSpawnedFor,
		&recurrence.NextSpawnAt,
		&recurrence.CreatedAt,
		&recurrence.UpdatedAt,
	); err != nil {
		return nil, err
	}

	recurrence.Repetition = taskdomain.Repetition(recurrenceType)
	if len(recurrenceConfig) > 0 {
		recurrence.RepetitionConfig = append(json.RawMessage(nil), recurrenceConfig...)
	}
	if lastSpawnedFor.Valid {
		value := taskdomain.DateOnlyUTC(lastSpawnedFor.Time)
		recurrence.LastSpawnedFor = &value
	}

	return &recurrence, nil
}
