package accounting

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestPostgresStoreInsertAcceptedShare_Share(t *testing.T) {
	var got []interface{}
	store := newPostgresStoreWithExec(func(_ context.Context, _ string, args ...interface{}) error {
		got = args
		return nil
	})

	err := store.InsertAcceptedShare(context.Background(), ShareRecord{
		Worker:        "worker.001",
		Diff:          12.5,
		Time:          time.Date(2026, 2, 18, 10, 0, 0, 0, time.FixedZone("LOCAL", 3*3600)),
		Type:          "share",
		SubmissionKey: "job|en2|ntime|nonce",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 5 {
		t.Fatalf("expected 5 args, got %d", len(got))
	}
	if got[0] != "worker.001" {
		t.Fatalf("unexpected worker: %v", got[0])
	}
	if got[1] != 12.5 {
		t.Fatalf("unexpected diff: %v", got[1])
	}
	ts, ok := got[2].(time.Time)
	if !ok || ts.Location() != time.UTC {
		t.Fatalf("expected UTC timestamp, got %T %v", got[2], got[2])
	}
	if got[3] != "share" {
		t.Fatalf("unexpected type: %v", got[3])
	}
}

func TestPostgresStoreInsertAcceptedShare_Block(t *testing.T) {
	var gotType interface{}
	store := newPostgresStoreWithExec(func(_ context.Context, _ string, args ...interface{}) error {
		gotType = args[3]
		return nil
	})

	err := store.InsertAcceptedShare(context.Background(), ShareRecord{
		Worker:        "worker.block",
		Diff:          50000,
		Time:          time.Now(),
		Type:          "block",
		SubmissionKey: "x",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotType != "block" {
		t.Fatalf("unexpected type: %v", gotType)
	}
}

func TestPostgresStoreInsertAcceptedShare_DuplicateError(t *testing.T) {
	store := newPostgresStoreWithExec(func(_ context.Context, _ string, _ ...interface{}) error {
		return errors.New(`duplicate key value violates unique constraint "shares_submission_key_key"`)
	})

	err := store.InsertAcceptedShare(context.Background(), ShareRecord{
		Worker:        "worker.001",
		Diff:          1,
		Time:          time.Now(),
		Type:          "share",
		SubmissionKey: "dup-key",
	})
	if err == nil {
		t.Fatalf("expected error")
	}
	if !strings.Contains(err.Error(), "insert share") {
		t.Fatalf("expected wrapped insert error, got %v", err)
	}
}
