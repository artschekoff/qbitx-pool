package accounting

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgresStore struct {
	pool   *pgxpool.Pool
	poolID string
	execFn func(ctx context.Context, sql string, args ...interface{}) error
}

func newPostgresStoreWithExec(execFn func(ctx context.Context, sql string, args ...interface{}) error) *PostgresStore {
	return &PostgresStore{execFn: execFn, poolID: "default"}
}

func NewPostgresStore(databaseURL string, poolID string) (*PostgresStore, error) {
	pool, err := pgxpool.New(context.Background(), databaseURL)
	if err != nil {
		return nil, fmt.Errorf("postgres connect: %w", err)
	}
	return &PostgresStore{
		pool:   pool,
		poolID: poolID,
		execFn: func(ctx context.Context, sql string, args ...interface{}) error {
			_, err := pool.Exec(ctx, sql, args...)
			return err
		},
	}, nil
}

func (s *PostgresStore) InsertAcceptedShare(ctx context.Context, r ShareRecord) error {
	const q = `
	INSERT INTO shares (pool_id, worker, diff, submitted_at, share_type, submission_key, block_hash)
	VALUES ($1, $2, $3, $4, $5, $6, $7)
	`
	if r.Type != "share" && r.Type != "block" {
		return fmt.Errorf("invalid share type: %s", r.Type)
	}
	if r.Diff <= 0 {
		return fmt.Errorf("invalid diff: %.8f", r.Diff)
	}
	var blockHashPtr interface{}
	if r.BlockHash == "" {
		blockHashPtr = nil
	} else {
		blockHashPtr = r.BlockHash
	}
	err := s.execFn(ctx, q, s.poolID, r.Worker, r.Diff, r.Time.UTC(), r.Type, r.SubmissionKey, blockHashPtr)
	if err != nil {
		return fmt.Errorf("insert share: %w", err)
	}
	return nil
}

func (s *PostgresStore) Close() {
	if s.pool != nil {
		s.pool.Close()
	}
}
