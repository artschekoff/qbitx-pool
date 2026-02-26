package config

import (
	"fmt"
	"math"
	"os"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Pool           PoolCfg       `yaml:"pool"`
	Stratum        StratumCfg    `yaml:"stratum"`
	Daemon         DaemonCfg     `yaml:"daemon"`
	Coinbase       CoinbaseCfg   `yaml:"coinbase"`
	Jobs           JobsCfg       `yaml:"jobs"`
	Difficulty     DifficultyCfg `yaml:"difficulty"`
	Payouts        PayoutsCfg    `yaml:"payouts"`
	MaxSPS         int           `yaml:"max_sps"`
	MaxSPSGraceSec int           `yaml:"max_sps_grace_sec"`
	SharesFile     string        `yaml:"shares_file"` // deprecated
	DatabaseURL    string        `yaml:"-"`
	Log            LogCfg        `yaml:"log"`
}

type StratumCfg struct {
	Listen  string `yaml:"listen"`
	MaxConn int    `yaml:"max_conn"`
}

type PoolCfg struct {
	ID string `yaml:"id"`
}

type DaemonCfg struct {
	URL  string `yaml:"url"`
	User string `yaml:"user"`
	Pass string `yaml:"pass"`
}

type CoinbaseCfg struct {
	Address             string  `yaml:"address"`
	Tag                 string  `yaml:"tag"`
	Reward              int64   `yaml:"reward"`
	PayoutSPKHex        string  `yaml:"payout_spk_hex"`
	DeveloperFeePercent float64 `yaml:"developer_fee_percent"`
	DeveloperFeeAddress string  `yaml:"developer_fee_address"`
}

type JobsCfg struct {
	PollInterval     int  `yaml:"poll_interval"`
	NotifyOnNewBlock bool `yaml:"notify_on_new_block"`
}

type DifficultyCfg struct {
	Start            float64 `yaml:"start"`
	GPUStart         float64 `yaml:"gpu_start"`
	ASICStart        float64 `yaml:"asic_start"`
	Min              float64 `yaml:"min"`
	Max              float64 `yaml:"max"`
	TargetTime       int     `yaml:"target_time"`
	RetargetInterval int     `yaml:"retarget_interval"`
}

type PayoutsCfg struct {
	Enabled        bool    `yaml:"enabled"`
	Scheme         string  `yaml:"scheme"`
	PPLNSWindow    int     `yaml:"pplns_window"` // deprecated for l-prop
	PoolFeePercent float64 `yaml:"pool_fee_percent"`
	MinPayoutSat   int64   `yaml:"min_payout_sat"`
	IntervalSec    int     `yaml:"interval_sec"`
	BlockMaturity  int64   `yaml:"block_maturity"`
}

type LogCfg struct {
	Level string `yaml:"level"`
	Dir   string `yaml:"dir"`
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config %s: %w", path, err)
	}
	cfg := &Config{
		Pool:           PoolCfg{ID: "default"},
		Stratum:        StratumCfg{Listen: ":3333", MaxConn: 4096},
		Jobs:           JobsCfg{PollInterval: 10, NotifyOnNewBlock: true},
		Difficulty:     DifficultyCfg{Start: 30000, GPUStart: 10, ASICStart: 30000, Min: 64, Max: 10000000, TargetTime: 10, RetargetInterval: 30},
		Payouts:        PayoutsCfg{Enabled: false, Scheme: "lprop", PPLNSWindow: 1000, PoolFeePercent: 0, MinPayoutSat: 100000, IntervalSec: 60, BlockMaturity: 100},
		MaxSPS:         300,
		MaxSPSGraceSec: 3,
		SharesFile:     "data/shares.json",
		Log:            LogCfg{Level: "info"},
	}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	if cfg.Pool.ID == "" {
		return nil, fmt.Errorf("pool.id is required")
	}
	cfg.DatabaseURL = os.Getenv("DATABASE_URL")
	if cfg.Coinbase.Address == "" {
		return nil, fmt.Errorf("coinbase.address is required")
	}
	if cfg.Coinbase.DeveloperFeePercent < 0 || cfg.Coinbase.DeveloperFeePercent >= 100 {
		return nil, fmt.Errorf("coinbase.developer_fee_percent must be in range [0, 100)")
	}
	if !hasAtMostTwoDecimals(cfg.Coinbase.DeveloperFeePercent) {
		return nil, fmt.Errorf("coinbase.developer_fee_percent must have at most 2 decimal places")
	}
	if cfg.Coinbase.DeveloperFeePercent > 0 && cfg.Coinbase.DeveloperFeeAddress == "" {
		return nil, fmt.Errorf("coinbase.developer_fee_address is required when developer_fee_percent > 0")
	}
	if cfg.Payouts.Scheme == "" {
		cfg.Payouts.Scheme = "lprop"
	}
	if cfg.Payouts.Scheme != "lprop" && cfg.Payouts.Scheme != "solo" {
		return nil, fmt.Errorf("payouts.scheme must be one of: lprop, solo")
	}
	if cfg.Payouts.PoolFeePercent < 0 || cfg.Payouts.PoolFeePercent >= 100 {
		return nil, fmt.Errorf("payouts.pool_fee_percent must be in range [0, 100)")
	}
	if !hasAtMostTwoDecimals(cfg.Payouts.PoolFeePercent) {
		return nil, fmt.Errorf("payouts.pool_fee_percent must have at most 2 decimal places")
	}
	if cfg.Payouts.MinPayoutSat < 0 {
		return nil, fmt.Errorf("payouts.min_payout_sat must be >= 0")
	}
	if cfg.Payouts.IntervalSec <= 0 {
		return nil, fmt.Errorf("payouts.interval_sec must be > 0")
	}
	if cfg.Payouts.BlockMaturity <= 0 {
		return nil, fmt.Errorf("payouts.block_maturity must be > 0")
	}
	return cfg, nil
}

func hasAtMostTwoDecimals(v float64) bool {
	return math.Abs(v*100-math.Round(v*100)) < 1e-9
}
