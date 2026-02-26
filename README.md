# Q-BitX Pool

Connection guide: [docs/user-connection.md](docs/user-connection.md)

## Table of Contents

- [Overview](#overview)
- [Quick Start](#quick-start)
- [Configuration](#configuration)
- [Payouts](#payouts)
- [Multi-Pool Setup](#multi-pool-setup)
- [Project Structure](#project-structure)

## Overview

This repository contains the Q-BitX Stratum mining pool service.

## Quick Start

```bash
cp .env.example .env
cp config.example.yaml config.yaml
make build
./bin/qbxpool config.yaml
```

## Configuration

Set unique pool identity per running instance:

```yaml
pool:
  id: "main"
```

Required environment variable:

```bash
DATABASE_URL="postgresql://<user>:<password>@localhost:5533/qbitx?schema=public"
```

The pool applies SQL migrations from `migrations/` automatically on startup.

## Payouts

Supported schemes:
- `lprop`
- `solo`

Example:

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

You can run multiple pool instances (for example `solo` and `lprop`) against one shared PostgreSQL database.

Requirements:

1. Each instance must use a different Stratum port.
2. Each instance must use a unique `pool.id`.
3. Keep each instance config consistent with its payout scheme (`solo` or `lprop`).

## Project Structure

- `cmd/pool` - pool server entrypoint
- `internal` - business logic
- `migrations` - PostgreSQL migrations
- `config.example.yaml` - example config
