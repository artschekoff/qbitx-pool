# Q-BitX Pool

**A Stratum mining-pool server for the Q-BitX chain — PostgreSQL-backed, multi-instance, with `lprop` and `solo` payout schemes.**

Q-BitX Pool accepts Stratum connections from miners, tracks submitted shares, and pays out as blocks mature. It applies its own SQL migrations on startup, and several instances (for example a `solo` pool and an `lprop` pool) can run side by side against one shared PostgreSQL database.

> **Connecting a miner?** See the [connection guide](docs/user-connection.md).

## Table of Contents

- [Quick Start](#quick-start)
- [Configuration](#configuration)
- [Payouts](#payouts)
- [Multi-Pool Setup](#multi-pool-setup)
- [Project Structure](#project-structure)

## Quick Start

```bash
cp .env.example .env
cp config.example.yaml config.yaml
make build
./bin/qbxpool config.yaml
```

The pool applies SQL migrations from `migrations/` automatically on startup.

## Configuration

Give each running instance a unique pool identity:

```yaml
pool:
  id: "main"
```

Required environment variable:

```bash
DATABASE_URL="postgresql://<user>:<password>@localhost:5533/qbitx?schema=public"
```

## Payouts

Supported schemes: `lprop` and `solo`.

```yaml
payouts:
  enabled: true
  scheme: "lprop"
  pool_fee_percent: 0
  min_payout_sat: 100000
  interval_sec: 60
  block_maturity: 100
```

## Multi-Pool Setup

You can run multiple pool instances against one shared PostgreSQL database. Requirements:

1. Each instance uses a different Stratum port.
2. Each instance uses a unique `pool.id`.
3. Each instance's config matches its payout scheme (`solo` or `lprop`).

## Project Structure

- `cmd/pool` — pool server entrypoint
- `internal` — business logic (shares, payouts, config, migrations runner)
- `migrations` — PostgreSQL migrations, applied on startup
- `config.example.yaml` — example config (`config.production.yaml` shows a production layout)

---

**Tech:** Go · Stratum · PostgreSQL.
