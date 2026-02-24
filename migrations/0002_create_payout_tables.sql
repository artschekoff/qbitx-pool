CREATE TABLE IF NOT EXISTS balances (
  worker TEXT PRIMARY KEY,
  amount_sat BIGINT NOT NULL DEFAULT 0 CHECK (amount_sat >= 0),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- NOTE: This migration assumes that a `shares` table with a primary key column `id`
-- already exists (for example, created in an earlier migration such as 0001).
-- The foreign key block_settlements.block_share_id references shares(id).
CREATE TABLE IF NOT EXISTS block_settlements (
  block_share_id BIGINT PRIMARY KEY REFERENCES shares(id) ON DELETE CASCADE,
  block_submission_key TEXT NOT NULL UNIQUE,
  block_time TIMESTAMPTZ NOT NULL,
  reward_sat BIGINT NOT NULL,
  miner_reward_sat BIGINT NOT NULL,
  pplns_window INT NOT NULL,
  processed_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS payout_batches (
  id BIGSERIAL PRIMARY KEY,
  status TEXT NOT NULL CHECK (status IN ('pending', 'sent', 'failed')),
  txid TEXT,
  error TEXT,
  total_sat BIGINT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  finished_at TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS payout_batch_items (
  batch_id BIGINT NOT NULL REFERENCES payout_batches(id) ON DELETE CASCADE,
  worker TEXT NOT NULL,
  amount_sat BIGINT NOT NULL CHECK (amount_sat > 0),
  PRIMARY KEY (batch_id, worker)
);

CREATE INDEX IF NOT EXISTS idx_balances_amount ON balances(amount_sat DESC);
CREATE INDEX IF NOT EXISTS idx_payout_batches_status ON payout_batches(status, created_at DESC);
