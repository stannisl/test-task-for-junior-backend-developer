package task

import (
	"encoding/json"

	taskdomain "example.com/taskservice/internal/domain/task"
)

type CreateInput struct {
	Title       string
	Description string
	Status      taskdomain.Status
	Repetition  taskdomain.Repetition
	Config      json.RawMessage
}

type UpdateInput struct {
	Title       string
	Description string
	Status      taskdomain.Status
	Repetition  taskdomain.Repetition
	Config      json.RawMessage
}
