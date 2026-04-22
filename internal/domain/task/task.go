package task

import "time"

type Status string

const (
	StatusNew        Status = "new"
	StatusInProgress Status = "in_progress"
	StatusDone       Status = "done"
)

type Repetition string

const (
	RepetitionTypeDaily     Repetition = "daily"
	RepetitionTypeWeekly    Repetition = "weekly"
	RepetitionTypeEvenMonth Repetition = "even_month"
	RepetitionTypeOddMonth  Repetition = "odd_month"
	RepetitionTypeMonthDays Repetition = "month_days"
)

type Task struct {
	ID          int64      `json:"id"`
	Title       string     `json:"title"`
	Description string     `json:"description"`
	Status      Status     `json:"status"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	Repetition  Repetition `json:"repetition"`
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
		RepetitionTypeWeekly,
		RepetitionTypeMonthDays,
		RepetitionTypeEvenMonth,
		RepetitionTypeOddMonth:
		return true
	default:
		return false
	}
}
