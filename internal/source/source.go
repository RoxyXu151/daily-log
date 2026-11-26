package source

import (
	"context"
	"time"

	"daily-log/internal/model"
)

type Window struct {
	Since   time.Time
	Until   time.Time
	History bool
}

type Batch struct {
	Events  []model.Event `json:"events"`
	Details model.Object  `json:"details"`
}

type Collector func(context.Context, Window) (Batch, error)
