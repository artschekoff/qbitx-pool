ALTER TABLE shares
  ADD COLUMN IF NOT EXISTS block_hash TEXT;

CREATE UNIQUE INDEX IF NOT EXISTS idx_shares_block_hash_unique
  ON shares(block_hash)
  WHERE block_hash IS NOT NULL;

DO $$
BEGIN
  CREATE TYPE block_status AS ENUM ('found', 'immature', 'mature', 'orphan', 'settled');
EXCEPTION
  WHEN duplicate_object THEN
    NULL;
END;
$$;

CREATE TABLE IF NOT EXISTS block_states (
  block_share_id BIGINT PRIMARY KEY REFERENCES shares(id) ON DELETE CASCADE,
  block_hash TEXT NOT NULL UNIQUE,
  status block_status NOT NULL,
  confirmations BIGINT NOT NULL DEFAULT 0,
  last_error TEXT,
  last_checked_at TIMESTAMPTZ,
  matured_at TIMESTAMPTZ,
  settled_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_block_states_status_updated
  ON block_states(status, updated_at DESC);
