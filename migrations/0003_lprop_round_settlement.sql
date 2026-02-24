ALTER TABLE block_settlements
  ADD COLUMN IF NOT EXISTS round_start_share_id BIGINT;

ALTER TABLE block_settlements
  ADD COLUMN IF NOT EXISTS round_end_share_id BIGINT NOT NULL DEFAULT 0;

ALTER TABLE block_settlements
  ADD COLUMN IF NOT EXISTS shares_count INT NOT NULL DEFAULT 0;

ALTER TABLE block_settlements
  ADD COLUMN IF NOT EXISTS total_weight DOUBLE PRECISION NOT NULL DEFAULT 0;
