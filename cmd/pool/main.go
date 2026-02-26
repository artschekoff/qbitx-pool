package main

import (
	"context"
	"fmt"
	"log"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/joho/godotenv"
	"github.com/q-bitx/pool/internal/accounting"
	"github.com/q-bitx/pool/internal/config"
	"github.com/q-bitx/pool/internal/daemon"
	"github.com/q-bitx/pool/internal/job"
	"github.com/q-bitx/pool/internal/share"
	"github.com/q-bitx/pool/internal/stratum"
	"gopkg.in/yaml.v3"
)

func main() {
	log.SetFlags(log.Ldate | log.Ltime | log.Lmicroseconds | log.Lshortfile)
	_ = godotenv.Load(".env", ".env.prod")

	cfgPath := "config.yaml"
	if len(os.Args) > 1 {
		cfgPath = os.Args[1]
	}

	cfg, err := config.Load(cfgPath)
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	logCurrentConfig(cfgPath, cfg)

	log.Printf("Q-BitX Mining Pool starting")
	log.Printf("  pool_id  : %s", cfg.Pool.ID)
	log.Printf("  stratum  : %s", cfg.Stratum.Listen)
	log.Printf("  daemon   : %s", cfg.Daemon.URL)
	log.Printf("  payout   : %s", cfg.Coinbase.Address)
	log.Printf("  shares   : postgresql")
	log.Printf("  payouts  : enabled=%t", cfg.Payouts.Enabled)
	if err := validateDatabaseURL(cfg); err != nil {
		log.Fatalf("config: %v", err)
	}
	if err := accounting.RunMigrations(context.Background(), cfg.DatabaseURL, "migrations"); err != nil {
		log.Fatalf("migrations: %v", err)
	}
	log.Printf("  migrate  : up to date")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	rpc := daemon.NewClient(cfg.Daemon.URL, cfg.Daemon.User, cfg.Daemon.Pass)
	height, err := rpc.GetBlockCount()
	if err != nil {
		log.Printf("WARNING: daemon unreachable: %v (will retry)", err)
	} else {
		log.Printf("  height   : %d", height)
	}

	jm := job.NewMaker(cfg)
	go jm.Run(ctx)

	val := share.NewValidator(jm, rpc)
	store, err := accounting.NewPostgresStore(cfg.DatabaseURL, cfg.Pool.ID)
	if err != nil {
		log.Fatalf("share store: %v", err)
	}
	defer store.Close()

	if cfg.Payouts.Enabled {
		engine, err := accounting.NewPayoutEngine(cfg.DatabaseURL, cfg, rpc)
		if err != nil {
			log.Fatalf("payout engine: %v", err)
		}
		defer engine.Close()
		go engine.Run(ctx)
	}

	srv := stratum.NewServer(cfg, jm, val, store)
	go func() {
		if err := srv.ListenAndServe(); err != nil {
			log.Fatalf("stratum: %v", err)
		}
	}()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
	log.Println("shutting down...")
	cancel()
	time.Sleep(500 * time.Millisecond)
}

func validateDatabaseURL(cfg *config.Config) error {
	if cfg.DatabaseURL == "" {
		return fmt.Errorf("DATABASE_URL is required")
	}
	return nil
}

func logCurrentConfig(path string, cfg *config.Config) {
	redacted := *cfg
	if redacted.Daemon.Pass != "" {
		redacted.Daemon.Pass = "***"
	}

	raw, err := yaml.Marshal(&redacted)
	if err != nil {
		log.Printf("config dump: failed to marshal YAML: %v", err)
		return
	}

	log.Printf("Loaded config from %s:\n%s", path, strings.TrimSpace(string(raw)))
	log.Printf("DATABASE_URL: %s", redactDatabaseURL(cfg.DatabaseURL))
}

func redactDatabaseURL(databaseURL string) string {
	if strings.TrimSpace(databaseURL) == "" {
		return "<empty>"
	}

	u, err := url.Parse(databaseURL)
	if err != nil {
		return "<invalid DSN redacted>"
	}
	if u.User != nil {
		name := u.User.Username()
		if _, hasPassword := u.User.Password(); hasPassword {
			u.User = url.UserPassword(name, "***")
		} else {
			u.User = url.User(name)
		}
	}
	return u.String()
}
