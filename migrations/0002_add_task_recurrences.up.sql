CREATE TABLE IF NOT EXISTS task_recurrences (
	id BIGSERIAL PRIMARY KEY,
	title TEXT NOT NULL,
	description TEXT NOT NULL DEFAULT '',
	recurrence_type TEXT NOT NULL,
	recurrence_config JSONB NOT NULL DEFAULT '{}'::jsonb,
	is_active BOOLEAN NOT NULL DEFAULT TRUE,
	last_spawned_for DATE,
	next_spawn_at TIMESTAMPTZ NOT NULL,
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	CONSTRAINT chk_task_recurrences_type CHECK (
		recurrence_type IN ('daily', 'month_days', 'specific_dates', 'even_month', 'odd_month')
	)
);

ALTER TABLE tasks
	ADD COLUMN IF NOT EXISTS recurrence_id BIGINT REFERENCES task_recurrences (id) ON DELETE SET NULL,
	ADD COLUMN IF NOT EXISTS scheduled_for DATE;

CREATE INDEX IF NOT EXISTS idx_task_recurrences_due
	ON task_recurrences (next_spawn_at)
	WHERE is_active = TRUE;

CREATE UNIQUE INDEX IF NOT EXISTS uq_tasks_recurrence_scheduled_for
	ON tasks (recurrence_id, scheduled_for)
	WHERE recurrence_id IS NOT NULL AND scheduled_for IS NOT NULL;
