package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/joho/godotenv"
	"github.com/q-bitx/pool/internal/accounting"
	"github.com/q-bitx/pool/internal/config"
	"github.com/q-bitx/pool/internal/daemon"
	"github.com/q-bitx/pool/internal/job"
	"github.com/q-bitx/pool/internal/share"
	"github.com/q-bitx/pool/internal/stratum"
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

	log.Printf("Q-BitX Mining Pool starting")
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
	store, err := accounting.NewPostgresStore(cfg.DatabaseURL)
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
