CREATE TABLE IF NOT EXISTS shares (
  id BIGSERIAL PRIMARY KEY,
  worker TEXT NOT NULL,
  diff DOUBLE PRECISION NOT NULL CHECK (diff > 0),
  submitted_at TIMESTAMPTZ NOT NULL,
  share_type TEXT NOT NULL CHECK (share_type IN ('share', 'block')),
  submission_key TEXT NOT NULL UNIQUE
);

CREATE INDEX IF NOT EXISTS idx_shares_worker_time ON shares(worker, submitted_at DESC);
CREATE INDEX IF NOT EXISTS idx_shares_type_time ON shares(share_type, submitted_at DESC);
