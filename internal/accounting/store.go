package accounting

import (
	"context"
	"time"
)

type ShareRecord struct {
	Worker        string
	Diff          float64
	Time          time.Time
	Type          string
	SubmissionKey string
}

type ShareStore interface {
	InsertAcceptedShare(ctx context.Context, r ShareRecord) error
	Close()
}
