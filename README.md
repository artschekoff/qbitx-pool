# Q-BitX Pool

Local `pool` repository for the mining pool service.

## Quick Start

```bash
cp .env.example .env
make build
./bin/qbxpool config.yaml
```

## Required Environment Variables

`DATABASE_URL` is required. Secret-safe examples:

```bash
# local host-run
DATABASE_URL="postgresql://<user>:<password>@localhost:5533/qbitx?schema=public"

# production
DATABASE_URL="postgresql://<user>:<password>@<host>:5432/<db>"
```

The app tries to load `.env` and `.env.prod` at startup (best effort), but process env vars have priority.

## Migrations

SQL migrations are in `migrations/`.
On startup, `qbxpool` automatically applies new `*.sql` migrations to PostgreSQL and tracks progress in `schema_migrations`.

## Docker Compose

The `pool` service uses `env_file: .env`.
Do not store real credentials in the repository.

## Cooperative Mining Flow

1. Miners connect over Stratum (`mining.subscribe` / `authorize` / `submit`).
2. The pool sends shared jobs and validates submitted shares.
3. Every accepted `share` and `block` is written to PostgreSQL (`shares`).
4. If a submitted share is a valid block, the pool sends it to the daemon (`submitblock`).
5. The payout engine then distributes rewards using the configured scheme (`lprop` or `solo`).

### Worker Format and Payout Address

Payouts use worker names in this format:

`<payout_address>.<rig_id>`

Examples:
- `MWnx...abc.rig01`
- `MWnx...abc.gpu-farm-2`

The payout address is the part before the first dot.

## Payouts (L-PROP / SOLO + Ledger)

Implemented strategies:
- Full L-PROP round: use all shares between previous found block and current found block.
- SOLO: reward only the miner who found the block.
- Share weight: assigned share difficulty (`diff`) at accept time.
- Block reward is split according to selected scheme.

Block lifecycle:
1. Found block is recorded as `found`
2. State moves to `immature` as confirmations grow
3. State becomes `mature` after `payouts.block_maturity` confirmations
4. If block disappears/reorgs, state becomes `orphan`
5. Only `mature` blocks are settled

Per-block settlement:
1. `block_reward = coinbase.reward`
2. Subtract developer fee (`coinbase.developer_fee_percent`)
3. Subtract pool fee from the remainder (`payouts.pool_fee_percent`)
4. Distribute the result:
   - `lprop`: by round weights
   - `solo`: 100% to block finder
5. Write credits to `balances` and settlement record to `block_settlements`

Payout sending:
1. Every `payouts.interval_sec`, select balances `>= payouts.min_payout_sat`
2. Create payout batch and call daemon RPC `sendmany`
3. On success, mark batch as `sent` and store `txid`
4. On failure, mark batch as `failed` and refund amounts back to `balances`

## Developer Fee (coinbase-level)

You can enable developer fee in `config.yaml`:

```yaml
coinbase:
  developer_fee_percent: 1
  developer_fee_address: "<your_dev_address>"
```

If `developer_fee_percent > 0`, address is required. Block coinbase is split into two outputs: primary payout and dev fee.

## Payout Config

```yaml
payouts:
  enabled: true
  scheme: "lprop"
  pool_fee_percent: 0
  min_payout_sat: 100000
  interval_sec: 60
  block_maturity: 100
```

- `enabled`: enable payout engine
- `scheme`: payout algorithm (`lprop` or `solo`)
- `pool_fee_percent`: operator fee
- `min_payout_sat`: minimum balance to send
- `interval_sec`: settlement + payout loop interval
- `block_maturity`: confirmations required before settlement

## Structure

- `cmd/pool` - pool server entrypoint
- `internal` - internal business logic
- `migrations` - DB SQL migrations
- `config.example.yaml` - sample YAML config
