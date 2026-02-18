package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Stratum        StratumCfg    `yaml:"stratum"`
	Daemon         DaemonCfg     `yaml:"daemon"`
	Coinbase       CoinbaseCfg   `yaml:"coinbase"`
	Jobs           JobsCfg       `yaml:"jobs"`
	Difficulty     DifficultyCfg `yaml:"difficulty"`
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

type DaemonCfg struct {
	URL  string `yaml:"url"`
	User string `yaml:"user"`
	Pass string `yaml:"pass"`
}

type CoinbaseCfg struct {
	Address      string `yaml:"address"`
	Tag          string `yaml:"tag"`
	Reward       int64  `yaml:"reward"`
	PayoutSPKHex string `yaml:"payout_spk_hex"`
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
		Stratum:        StratumCfg{Listen: ":3333", MaxConn: 4096},
		Jobs:           JobsCfg{PollInterval: 10, NotifyOnNewBlock: true},
		Difficulty:     DifficultyCfg{Start: 30000, GPUStart: 10, ASICStart: 30000, Min: 64, Max: 10000000, TargetTime: 10, RetargetInterval: 30},
		MaxSPS:         300,
		MaxSPSGraceSec: 3,
		SharesFile:     "data/shares.json",
		Log:            LogCfg{Level: "info"},
	}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	cfg.DatabaseURL = os.Getenv("DATABASE_URL")
	if cfg.Coinbase.Address == "" {
		return nil, fmt.Errorf("coinbase.address is required")
	}
	return cfg, nil
}
