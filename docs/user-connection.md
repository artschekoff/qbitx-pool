# User Miner Connection Guide

This guide is only for users connecting a miner to an existing pool server.

## 1. Miner connection settings

Stratum URL:

```text
stratum+tcp://<POOL_SERVER_IP>:3333
```

Username format (`-u`):

```text
<wallet_address>.<worker_name>
```

Examples:
- `MUwWzHHCFkW2V62N8xgXX1rNaDXQ6Hxj19.rig01`
- `MWnxrbCdBdtUP2M5NZ7v5bYrbZ78ZsMBzh.asic-01`

Password (`-p`): any value, commonly `x`.

## 2. Miner command examples

`cpuminer`:

```bash
minerd -a sha256d -o stratum+tcp://<POOL_SERVER_IP>:3333 -u <wallet_address>.<worker_name> -p x
```

Generic `bfgminer/cgminer` template:

```bash
<miner_binary> -o stratum+tcp://<POOL_SERVER_IP>:3333 -u <wallet_address>.<worker_name> -p x
```

## 3. Quick self-check

1. Miner process can resolve and reach `<POOL_SERVER_IP>:3333`.
2. Miner shows successful Stratum subscribe/authorize.
3. Miner starts submitting shares without repeated auth/reconnect errors.
