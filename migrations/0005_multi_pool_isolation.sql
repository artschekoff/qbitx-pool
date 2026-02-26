-- Add pool scoping so multiple pool instances can safely share one database.

ALTER TABLE shares
  ADD COLUMN IF NOT EXISTS pool_id TEXT NOT NULL DEFAULT 'default';

ALTER TABLE balances
  ADD COLUMN IF NOT EXISTS pool_id TEXT NOT NULL DEFAULT 'default';

ALTER TABLE block_settlements
  ADD COLUMN IF NOT EXISTS pool_id TEXT NOT NULL DEFAULT 'default';

ALTER TABLE block_states
  ADD COLUMN IF NOT EXISTS pool_id TEXT NOT NULL DEFAULT 'default';

ALTER TABLE payout_batches
  ADD COLUMN IF NOT EXISTS pool_id TEXT NOT NULL DEFAULT 'default';

ALTER TABLE payout_batch_items
  ADD COLUMN IF NOT EXISTS pool_id TEXT NOT NULL DEFAULT 'default';

-- shares uniqueness must be per pool.
ALTER TABLE shares DROP CONSTRAINT IF EXISTS shares_submission_key_key;
DROP INDEX IF EXISTS shares_submission_key_key;
DROP INDEX IF EXISTS idx_shares_block_hash_unique;

CREATE UNIQUE INDEX IF NOT EXISTS idx_shares_pool_submission_key_unique
  ON shares(pool_id, submission_key);

CREATE UNIQUE INDEX IF NOT EXISTS idx_shares_pool_block_hash_unique
  ON shares(pool_id, block_hash)
  WHERE block_hash IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_shares_pool_type_time
  ON shares(pool_id, share_type, submitted_at DESC);

-- balances key must be per pool.
ALTER TABLE balances DROP CONSTRAINT IF EXISTS balances_pkey;
ALTER TABLE balances ADD PRIMARY KEY (pool_id, worker);

CREATE INDEX IF NOT EXISTS idx_balances_pool_amount
  ON balances(pool_id, amount_sat DESC);

-- block_settlements uniqueness must be per pool.
ALTER TABLE block_settlements DROP CONSTRAINT IF EXISTS block_settlements_block_submission_key_key;
DROP INDEX IF EXISTS block_settlements_block_submission_key_key;
ALTER TABLE block_settlements ADD CONSTRAINT block_settlements_pool_submission_key_key UNIQUE (pool_id, block_submission_key);

CREATE INDEX IF NOT EXISTS idx_block_settlements_pool_share
  ON block_settlements(pool_id, block_share_id);

-- block_states block hash must be unique per pool.
ALTER TABLE block_states DROP CONSTRAINT IF EXISTS block_states_block_hash_key;
DROP INDEX IF EXISTS block_states_block_hash_key;
CREATE UNIQUE INDEX IF NOT EXISTS idx_block_states_pool_hash_unique
  ON block_states(pool_id, block_hash);

CREATE INDEX IF NOT EXISTS idx_block_states_pool_status_updated
  ON block_states(pool_id, status, updated_at DESC);

-- payout batches/items must be queryable per pool.
ALTER TABLE payout_batch_items DROP CONSTRAINT IF EXISTS payout_batch_items_pkey;
ALTER TABLE payout_batch_items ADD PRIMARY KEY (pool_id, batch_id, worker);

CREATE INDEX IF NOT EXISTS idx_payout_batches_pool_status_created
  ON payout_batches(pool_id, status, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_payout_batch_items_pool_batch
  ON payout_batch_items(pool_id, batch_id);
