package task

import (
	"encoding/json"
	"time"
)

type Status string

const (
	StatusNew        Status = "new"
	StatusInProgress Status = "in_progress"
	StatusDone       Status = "done"
)

type Repetition string

const (
	RepetitionTypeDaily         Repetition = "daily"
	RepetitionTypeEvenMonth     Repetition = "even_month"
	RepetitionTypeOddMonth      Repetition = "odd_month"
	RepetitionTypeMonthDays     Repetition = "month_days"
	RepetitionTypeSpecificDates Repetition = "specific_dates"
)

type Task struct {
	ID               int64           `json:"id"`
	Title            string          `json:"title"`
	Description      string          `json:"description"`
	Status           Status          `json:"status"`
	CreatedAt        time.Time       `json:"created_at"`
	UpdatedAt        time.Time       `json:"updated_at"`
	RecurrenceID     *int64          `json:"recurrence_id,omitempty"`
	ScheduledFor     *time.Time      `json:"scheduled_for,omitempty"`
	Repetition       Repetition      `json:"repetition_type,omitempty"`
	RepetitionConfig json.RawMessage `json:"repetition_config,omitempty"`
}

type Recurrence struct {
	ID               int64
	Title            string
	Description      string
	Repetition       Repetition
	RepetitionConfig json.RawMessage
	IsActive         bool
	LastSpawnedFor   *time.Time
	NextSpawnAt      time.Time
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

type ListInput struct {
	Limit  int
	Offset int
}

func (s Status) Valid() bool {
	switch s {
	case StatusNew, StatusInProgress, StatusDone:
		return true
	default:
		return false
	}
}

func (r Repetition) Valid() bool {
	switch r {
	case RepetitionTypeDaily,
		RepetitionTypeMonthDays,
		RepetitionTypeEvenMonth,
		RepetitionTypeOddMonth,
		RepetitionTypeSpecificDates:
		return true
	default:
		return false
	}
}
