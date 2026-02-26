package accounting

import (
	"context"
	"fmt"
	"log"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/q-bitx/pool/internal/config"
)

type DaemonSender interface {
	SendMany(amounts map[string]float64) (string, error)
	LookupBlockConfirmations(hash string) (int64, bool, error)
}

const (
	blockStateFound    = "found"
	blockStateImmature = "immature"
	blockStateMature   = "mature"
	blockStateOrphan   = "orphan"
	blockStateSettled  = "settled"
)

type PayoutEngine struct {
	pool   *pgxpool.Pool
	cfg    *config.Config
	poolID string
	sender DaemonSender
}

func NewPayoutEngine(databaseURL string, cfg *config.Config, sender DaemonSender) (*PayoutEngine, error) {
	pool, err := pgxpool.New(context.Background(), databaseURL)
	if err != nil {
		return nil, fmt.Errorf("postgres connect payout engine: %w", err)
	}
	return &PayoutEngine{pool: pool, cfg: cfg, poolID: cfg.Pool.ID, sender: sender}, nil
}

func (e *PayoutEngine) Close() {
	if e.pool != nil {
		e.pool.Close()
	}
}

func (e *PayoutEngine) Run(ctx context.Context) {
	interval := time.Duration(e.cfg.Payouts.IntervalSec) * time.Second
	if interval <= 0 {
		interval = 60 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	_ = e.runCycle(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_ = e.runCycle(ctx)
		}
	}
}

func (e *PayoutEngine) runCycle(ctx context.Context) error {
	if err := e.syncBlockStates(ctx); err != nil {
		log.Printf("[payout] block state sync error: %v", err)
		return err
	}

	settled, err := e.settleAllBlocks(ctx)
	if err != nil {
		log.Printf("[payout] settle error: %v", err)
		return err
	}
	if settled > 0 {
		log.Printf("[payout] settled blocks: %d", settled)
	}

	sent, err := e.trySendPayoutBatch(ctx)
	if err != nil {
		log.Printf("[payout] payout batch error: %v", err)
		return err
	}
	if sent {
		log.Printf("[payout] payout batch sent")
	}
	return nil
}

func (e *PayoutEngine) syncBlockStates(ctx context.Context) error {
	if _, err := e.pool.Exec(ctx, `
INSERT INTO block_states (block_share_id, pool_id, block_hash, status, confirmations, created_at, updated_at)
SELECT s.id, $1, s.block_hash, $2, 0, NOW(), NOW()
FROM shares s
WHERE s.pool_id = $1
  AND s.share_type = 'block'
  AND s.block_hash IS NOT NULL
  AND s.block_hash <> ''
  AND NOT EXISTS (
    SELECT 1 FROM block_states bs WHERE bs.block_share_id = s.id
  )
`, e.poolID, blockStateFound); err != nil {
		return fmt.Errorf("insert block states: %w", err)
	}

	rows, err := e.pool.Query(ctx, `
SELECT block_share_id, block_hash
FROM block_states
WHERE pool_id = $1
  AND status IN ($2, $3, $4)
ORDER BY block_share_id ASC
`, e.poolID, blockStateFound, blockStateImmature, blockStateMature)
	if err != nil {
		return fmt.Errorf("load block states: %w", err)
	}
	defer rows.Close()

	type blockStateRow struct {
		shareID int64
		hash    string
	}
	var blocks []blockStateRow
	for rows.Next() {
		var b blockStateRow
		if err := rows.Scan(&b.shareID, &b.hash); err != nil {
			return fmt.Errorf("scan block state: %w", err)
		}
		blocks = append(blocks, b)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate block states: %w", err)
	}

	for _, b := range blocks {
		confirmations, exists, err := e.sender.LookupBlockConfirmations(b.hash)
		if err != nil {
			if _, updErr := e.pool.Exec(ctx, `
UPDATE block_states
SET last_error = $2, last_checked_at = NOW(), updated_at = NOW()
WHERE block_share_id = $1 AND pool_id = $3
`, b.shareID, truncate(err.Error(), 1024), e.poolID); updErr != nil {
				return fmt.Errorf("update block state error for share %d: %w", b.shareID, updErr)
			}
			continue
		}

		nextStatus := blockStateFound
		if !exists || confirmations < 0 {
			nextStatus = blockStateOrphan
		} else if confirmations >= e.cfg.Payouts.BlockMaturity {
			nextStatus = blockStateMature
		} else if confirmations > 0 {
			nextStatus = blockStateImmature
		}

		if _, err := e.pool.Exec(ctx, `
UPDATE block_states
SET status = $2,
    confirmations = $3,
    last_error = NULL,
    last_checked_at = NOW(),
    matured_at = CASE WHEN $2 = $4 THEN COALESCE(matured_at, NOW()) ELSE matured_at END,
    updated_at = NOW()
WHERE block_share_id = $1 AND pool_id = $5
`, b.shareID, nextStatus, confirmations, blockStateMature, e.poolID); err != nil {
			return fmt.Errorf("update block state for share %d: %w", b.shareID, err)
		}
	}
	return nil
}

func (e *PayoutEngine) settleAllBlocks(ctx context.Context) (int, error) {
	count := 0
	for {
		ok, err := e.settleNextBlock(ctx)
		if err != nil {
			return count, err
		}
		if !ok {
			return count, nil
		}
		count++
	}
}

func (e *PayoutEngine) settleNextBlock(ctx context.Context) (bool, error) {
	tx, err := e.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return false, fmt.Errorf("begin settle tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var blockShareID int64
	var blockTime time.Time
	var blockKey string
	var blockWorker string
	var blockDiff float64
	var blockHash string
	row := tx.QueryRow(ctx, `
SELECT s.id, s.submitted_at, s.submission_key, s.worker, s.diff, s.block_hash
FROM shares s
JOIN block_states bst ON bst.block_share_id = s.id
WHERE s.pool_id = $1
  AND bst.pool_id = $1
  AND s.share_type = 'block'
  AND bst.status = $2
  AND NOT EXISTS (
    SELECT 1 FROM block_settlements bs WHERE bs.block_share_id = s.id AND bs.pool_id = $1
  )
ORDER BY s.id ASC
LIMIT 1
FOR UPDATE SKIP LOCKED
`, e.poolID, blockStateMature)
	err = row.Scan(&blockShareID, &blockTime, &blockKey, &blockWorker, &blockDiff, &blockHash)
	if err != nil {
		if err == pgx.ErrNoRows {
			return false, nil
		}
		return false, fmt.Errorf("select unsettled block: %w", err)
	}

	blockRewardSat := e.cfg.Coinbase.Reward
	if blockRewardSat <= 0 {
		return false, fmt.Errorf("block reward must be positive, got %d", blockRewardSat)
	}
	devFeeSat := calcFeeSat(blockRewardSat, e.cfg.Coinbase.DeveloperFeePercent)
	afterDevSat := blockRewardSat - devFeeSat
	if afterDevSat <= 0 {
		return false, fmt.Errorf("reward after developer fee <= 0")
	}
	poolFeeSat := calcFeeSat(afterDevSat, e.cfg.Payouts.PoolFeePercent)
	minerRewardSat := afterDevSat - poolFeeSat
	if minerRewardSat <= 0 {
		return false, fmt.Errorf("reward after pool fee <= 0")
	}

	alloc := map[string]int64{}
	roundStartShareID := int64(0)
	roundEndShareID := int64(0)
	sharesCount := 0
	totalWeight := 0.0

	switch e.cfg.Payouts.Scheme {
	case "solo":
		winner := payoutAddressFromWorker(blockWorker)
		if winner == "" {
			return false, fmt.Errorf("solo settlement: invalid winner worker %q: expected '<address>.<rig_id>' with a non-empty address portion", blockWorker)
		}
		alloc[winner] = minerRewardSat
		roundStartShareID = blockShareID
		roundEndShareID = blockShareID
		sharesCount = 1
		if blockDiff > 0 {
			totalWeight = blockDiff
		}
	case "lprop":
		var prevBlockShareID int64
		if err := tx.QueryRow(ctx, `
SELECT COALESCE(MAX(id), 0)
FROM shares
WHERE share_type = 'block'
  AND pool_id = $2
  AND id < $1
`, blockShareID, e.poolID).Scan(&prevBlockShareID); err != nil {
			return false, fmt.Errorf("load previous found block: %w", err)
		}

		rows, err := tx.Query(ctx, `
SELECT worker, diff
FROM shares
WHERE pool_id = $1
  AND id > $2
  AND id <= $3
  AND share_type IN ('share', 'block')
ORDER BY id ASC
`, e.poolID, prevBlockShareID, blockShareID)
		if err != nil {
			return false, fmt.Errorf("load l-prop round shares: %w", err)
		}
		defer rows.Close()

		weights := make(map[string]float64)
		for rows.Next() {
			var worker string
			var diff float64
			if err := rows.Scan(&worker, &diff); err != nil {
				return false, fmt.Errorf("scan round share: %w", err)
			}
			address := payoutAddressFromWorker(worker)
			if address == "" || diff <= 0 {
				continue
			}
			weights[address] += diff
			sharesCount++
		}
		if err := rows.Err(); err != nil {
			return false, fmt.Errorf("iterate round shares: %w", err)
		}
		if len(weights) == 0 {
			return false, fmt.Errorf("no valid shares in l-prop round for block %d", blockShareID)
		}
		for _, w := range weights {
			totalWeight += w
		}
		alloc = allocateByWeight(minerRewardSat, weights)
		roundStartShareID = prevBlockShareID + 1
		roundEndShareID = blockShareID
	default:
		return false, fmt.Errorf("unsupported payout scheme: %s", e.cfg.Payouts.Scheme)
	}

	if len(alloc) == 0 {
		return false, fmt.Errorf("allocation is empty for block %d", blockShareID)
	}

	for worker, amount := range alloc {
		if amount <= 0 {
			continue
		}
		if _, err := tx.Exec(ctx, `
INSERT INTO balances (pool_id, worker, amount_sat, updated_at)
VALUES ($1, $2, $3, NOW())
ON CONFLICT (pool_id, worker)
DO UPDATE SET amount_sat = balances.amount_sat + EXCLUDED.amount_sat, updated_at = NOW()
`, e.poolID, worker, amount); err != nil {
			return false, fmt.Errorf("upsert balance %s: %w", worker, err)
		}
	}

	if _, err := tx.Exec(ctx, `
INSERT INTO block_settlements (
  block_share_id, pool_id, block_submission_key, block_time, reward_sat, miner_reward_sat, pplns_window, processed_at,
  round_start_share_id, round_end_share_id, shares_count, total_weight
)
VALUES ($1, $2, $3, $4, $5, $6, $7, NOW(), $8, $9, $10, $11)
`, blockShareID, e.poolID, blockKey, blockTime, blockRewardSat, minerRewardSat, 0, roundStartShareID, roundEndShareID, sharesCount, totalWeight); err != nil {
		return false, fmt.Errorf("insert block settlement: %w", err)
	}

	if _, err := tx.Exec(ctx, `
UPDATE block_states
SET status = $2, settled_at = NOW(), updated_at = NOW()
WHERE block_share_id = $1 AND pool_id = $3
`, blockShareID, blockStateSettled, e.poolID); err != nil {
		return false, fmt.Errorf("mark block settled share_id=%d hash=%s: %w", blockShareID, blockHash, err)
	}

	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("commit settle tx: %w", err)
	}
	return true, nil
}

func (e *PayoutEngine) trySendPayoutBatch(ctx context.Context) (bool, error) {
	type item struct {
		worker string
		amount int64
	}

	tx, err := e.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return false, fmt.Errorf("begin payout tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	rows, err := tx.Query(ctx, `
SELECT worker, amount_sat
FROM balances
WHERE pool_id = $1
  AND amount_sat >= $2
ORDER BY amount_sat DESC
FOR UPDATE SKIP LOCKED
`, e.poolID, e.cfg.Payouts.MinPayoutSat)
	if err != nil {
		return false, fmt.Errorf("select payout balances: %w", err)
	}
	defer rows.Close()

	var selected []item
	var total int64
	for rows.Next() {
		var it item
		if err := rows.Scan(&it.worker, &it.amount); err != nil {
			return false, fmt.Errorf("scan payout balance: %w", err)
		}
		if it.amount <= 0 {
			continue
		}
		selected = append(selected, it)
		total += it.amount
	}
	if err := rows.Err(); err != nil {
		return false, fmt.Errorf("iterate payout balances: %w", err)
	}
	if len(selected) == 0 {
		return false, nil
	}

	var batchID int64
	if err := tx.QueryRow(ctx, `
INSERT INTO payout_batches (pool_id, status, total_sat, created_at)
VALUES ($1, 'pending', $2, NOW())
RETURNING id
`, e.poolID, total).Scan(&batchID); err != nil {
		return false, fmt.Errorf("insert payout batch: %w", err)
	}

	for _, it := range selected {
		if _, err := tx.Exec(ctx, `
INSERT INTO payout_batch_items (pool_id, batch_id, worker, amount_sat)
VALUES ($1, $2, $3, $4)
`, e.poolID, batchID, it.worker, it.amount); err != nil {
			return false, fmt.Errorf("insert payout item: %w", err)
		}
		if _, err := tx.Exec(ctx, `
UPDATE balances
SET amount_sat = amount_sat - $3, updated_at = NOW()
WHERE pool_id = $1 AND worker = $2
`, e.poolID, it.worker, it.amount); err != nil {
			return false, fmt.Errorf("debit balance %s: %w", it.worker, err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("commit payout tx: %w", err)
	}

	amounts := make(map[string]float64, len(selected))
	for _, it := range selected {
		amounts[it.worker] = satToCoin(it.amount)
	}

	txid, err := e.sender.SendMany(amounts)
	if err != nil {
		if rbErr := e.markBatchFailedAndRefund(ctx, batchID, err.Error()); rbErr != nil {
			return false, fmt.Errorf("sendmany error: %v; refund error: %w", err, rbErr)
		}
		return false, fmt.Errorf("sendmany: %w", err)
	}

	if _, err := e.pool.Exec(ctx, `
UPDATE payout_batches
SET status = 'sent', txid = $3, finished_at = NOW()
WHERE id = $1 AND pool_id = $2
`, batchID, e.poolID, txid); err != nil {
		return false, fmt.Errorf("mark payout batch sent: %w", err)
	}
	return true, nil
}

func (e *PayoutEngine) markBatchFailedAndRefund(ctx context.Context, batchID int64, reason string) error {
	tx, err := e.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin refund tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	rows, err := tx.Query(ctx, `
SELECT worker, amount_sat
FROM payout_batch_items
WHERE pool_id = $1 AND batch_id = $2
`, e.poolID, batchID)
	if err != nil {
		return fmt.Errorf("load payout batch items: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var worker string
		var amount int64
		if err := rows.Scan(&worker, &amount); err != nil {
			return fmt.Errorf("scan payout batch item: %w", err)
		}
		if _, err := tx.Exec(ctx, `
UPDATE balances
SET amount_sat = amount_sat + $3, updated_at = NOW()
WHERE pool_id = $1 AND worker = $2
`, e.poolID, worker, amount); err != nil {
			return fmt.Errorf("refund worker %s: %w", worker, err)
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate payout batch items: %w", err)
	}

	if _, err := tx.Exec(ctx, `
UPDATE payout_batches
SET status = 'failed', error = $3, finished_at = NOW()
WHERE id = $1 AND pool_id = $2
`, batchID, e.poolID, truncate(reason, 1024)); err != nil {
		return fmt.Errorf("mark payout batch failed: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit refund tx: %w", err)
	}
	return nil
}

func payoutAddressFromWorker(worker string) string {
	worker = strings.TrimSpace(worker)
	if worker == "" {
		return ""
	}
	parts := strings.SplitN(worker, ".", 2)
	return strings.TrimSpace(parts[0])
}

func calcFeeSat(total int64, percent float64) int64 {
	if total <= 0 || percent <= 0 {
		return 0
	}
	if percent >= 100 {
		return total
	}
	bps := int64(math.Round(percent * 100))
	if bps <= 0 {
		return 0
	}
	fee := (total * bps) / 10000
	if fee < 0 {
		return 0
	}
	if fee > total {
		return total
	}
	return fee
}

func allocateByWeight(total int64, weights map[string]float64) map[string]int64 {
	type fracPart struct {
		worker string
		frac   float64
	}
	if total <= 0 || len(weights) == 0 {
		return map[string]int64{}
	}

	var sum float64
	for _, w := range weights {
		if w > 0 {
			sum += w
		}
	}
	if sum <= 0 {
		return map[string]int64{}
	}

	result := make(map[string]int64, len(weights))
	parts := make([]fracPart, 0, len(weights))
	var allocated int64
	for worker, w := range weights {
		if w <= 0 {
			continue
		}
		exact := float64(total) * (w / sum)
		base := int64(math.Floor(exact))
		result[worker] = base
		allocated += base
		parts = append(parts, fracPart{worker: worker, frac: exact - float64(base)})
	}

	rem := total - allocated
	if rem <= 0 {
		return result
	}
	sort.Slice(parts, func(i, j int) bool {
		if parts[i].frac == parts[j].frac {
			return parts[i].worker < parts[j].worker
		}
		return parts[i].frac > parts[j].frac
	})
	limit := rem
	if int(limit) > len(parts) {
		limit = int64(len(parts))
	}
	for i := int64(0); i < rem; i++ {
		p := parts[int(i)%len(parts)]
		result[p.worker]++
	}
	return result
}

func satToCoin(v int64) float64 {
	return float64(v) / 1e8
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max]
}
