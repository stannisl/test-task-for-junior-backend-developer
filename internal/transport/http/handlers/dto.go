package handlers

import (
	"encoding/json"
	"time"

	taskdomain "example.com/taskservice/internal/domain/task"
)

type taskMutationDTO struct {
	Title       string            `json:"title"`
	Description string            `json:"description"`
	Status      taskdomain.Status `json:"status"`

	Repetition taskdomain.Repetition `json:"repetition_type"`
	Config     json.RawMessage       `json:"repetition_config"`
}

type taskUpdateDTO struct {
	Title       string            `json:"title"`
	Description string            `json:"description"`
	Status      taskdomain.Status `json:"status"`
}

type taskDTO struct {
	ID          int64             `json:"id"`
	Title       string            `json:"title"`
	Description string            `json:"description"`
	Status      taskdomain.Status `json:"status"`
	CreatedAt   time.Time         `json:"created_at"`
	UpdatedAt   time.Time         `json:"updated_at"`

	RecurrenceID *int64                `json:"recurrence_id,omitempty"`
	ScheduledFor *string               `json:"scheduled_for,omitempty"`
	Repetition   taskdomain.Repetition `json:"repetition_type,omitempty"`
	Config       json.RawMessage       `json:"repetition_config,omitempty"`
}

func newTaskDTO(task *taskdomain.Task) taskDTO {
	var scheduledFor *string
	if task.ScheduledFor != nil {
		value := task.ScheduledFor.UTC().Format("2006-01-02")
		scheduledFor = &value
	}

	return taskDTO{
		ID:           task.ID,
		Title:        task.Title,
		Description:  task.Description,
		Status:       task.Status,
		CreatedAt:    task.CreatedAt,
		UpdatedAt:    task.UpdatedAt,
		RecurrenceID: task.RecurrenceID,
		ScheduledFor: scheduledFor,
		Repetition:   task.Repetition,
		Config:       task.RepetitionConfig,
	}
}
