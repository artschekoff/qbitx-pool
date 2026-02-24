package job

import (
	"context"
	"encoding/hex"
	"fmt"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"github.com/q-bitx/pool/internal/config"
	"github.com/q-bitx/pool/internal/daemon"
	"github.com/q-bitx/pool/internal/mining"
)

// ExtraNonce sizes (bytes).
const (
	ExtraNonce1Size = 4
	ExtraNonce2Size = 4
)

// Job is a single Stratum mining job.
type Job struct {
	ID           string
	PrevHash     string   // 64 hex chars, internal byte order
	Coinbase1    string   // hex before extranonce
	Coinbase2    string   // hex after extranonce
	MerkleBranch []string // hex hashes
	Version      string   // 8 hex chars LE
	Bits         string   // 8 hex chars
	Time         string   // 8 hex chars LE
	CleanJobs    bool

	// Kept for share validation
	Height        int64
	CoinbaseValue int64
	Target        string
	PrevHashBytes [32]byte
	BitsUint      uint32
	VersionInt    int32
	TimeUint      uint32
	TxHashes      [][32]byte
	RawTxData     [][]byte // raw serialised non-coinbase txs
}

// Maker polls the daemon and produces jobs.
type Maker struct {
	cfg     *config.Config
	client  *daemon.Client
	mu      sync.RWMutex
	current *Job
	seq     atomic.Int64

	subsMu sync.RWMutex
	subs   []chan *Job
}

// NewMaker creates a job maker.
func NewMaker(cfg *config.Config) *Maker {
	return &Maker{
		cfg:    cfg,
		client: daemon.NewClient(cfg.Daemon.URL, cfg.Daemon.User, cfg.Daemon.Pass),
	}
}

// Subscribe returns a channel that receives new jobs.
func (m *Maker) Subscribe() chan *Job {
	ch := make(chan *Job, 4)
	m.subsMu.Lock()
	m.subs = append(m.subs, ch)
	m.subsMu.Unlock()
	return ch
}

func (m *Maker) notify(j *Job) {
	m.subsMu.RLock()
	defer m.subsMu.RUnlock()
	for _, ch := range m.subs {
		select {
		case ch <- j:
		default:
		}
	}
}

// Current returns the latest job snapshot.
func (m *Maker) Current() *Job {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.current
}

// Run starts the GBT polling loop. Blocks until ctx is cancelled.
func (m *Maker) Run(ctx context.Context) {
	interval := time.Duration(m.cfg.Jobs.PollInterval) * time.Second
	if interval <= 0 {
		interval = 10 * time.Second
	}
	log.Printf("[jobmaker] polling daemon every %v", interval)

	var lastPrev string
	for {
		tpl, err := m.client.GetBlockTemplate()
		if err != nil {
			log.Printf("[jobmaker] getblocktemplate error: %v", err)
		} else {
			clean := tpl.PreviousBlockHash != lastPrev
			lastPrev = tpl.PreviousBlockHash
			j, err := m.buildJob(tpl, clean)
			if err != nil {
				log.Printf("[jobmaker] build job error: %v", err)
			} else {
				m.mu.Lock()
				m.current = j
				m.mu.Unlock()
				m.notify(j)
				if clean {
					log.Printf("[jobmaker] new tip height=%d prev=%s", j.Height, j.PrevHash[:16])
				}
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(interval):
		}
	}
}

func (m *Maker) buildJob(tpl *daemon.BlockTemplate, clean bool) (*Job, error) {
	id := fmt.Sprintf("%x", m.seq.Add(1))

	cb1, cb2 := mining.BuildCoinbaseTx(
		tpl.Height, tpl.CoinbaseValue,
		m.cfg.Coinbase.Address, m.cfg.Coinbase.Tag,
		m.cfg.Coinbase.PayoutSPKHex,
		m.cfg.Coinbase.DeveloperFeePercent,
		m.cfg.Coinbase.DeveloperFeeAddress,
	)

	txHashes := make([][32]byte, 0, len(tpl.Transactions))
	rawTxData := make([][]byte, 0, len(tpl.Transactions))
	for _, tx := range tpl.Transactions {
		h, err := mining.HexToHash32(tx.TxID)
		if err != nil {
			return nil, fmt.Errorf("parse txid: %w", err)
		}
		txHashes = append(txHashes, h)
		raw, err := hex.DecodeString(tx.Data)
		if err != nil {
			return nil, fmt.Errorf("decode tx data: %w", err)
		}
		rawTxData = append(rawTxData, raw)
	}

	branch := mining.MerkleBranch(txHashes)
	branchHex := make([]string, len(branch))
	for i, b := range branch {
		branchHex[i] = hex.EncodeToString(b[:])
	}

	prevHash, err := mining.HexToHash32Rev(tpl.PreviousBlockHash)
	if err != nil {
		return nil, fmt.Errorf("parse prevhash: %w", err)
	}

	bits := parseBitsHex(tpl.Bits)
	ts := uint32(tpl.CurTime)

	return &Job{
		ID:            id,
		PrevHash:      hex.EncodeToString(prevHash[:]),
		Coinbase1:     cb1,
		Coinbase2:     cb2,
		MerkleBranch:  branchHex,
		Version:       fmt.Sprintf("%08x", uint32(tpl.Version)),
		Bits:          tpl.Bits,
		Time:          fmt.Sprintf("%08x", ts),
		CleanJobs:     clean,
		Height:        tpl.Height,
		CoinbaseValue: tpl.CoinbaseValue,
		Target:        tpl.Target,
		PrevHashBytes: prevHash,
		BitsUint:      bits,
		VersionInt:    tpl.Version,
		TimeUint:      ts,
		TxHashes:      txHashes,
		RawTxData:     rawTxData,
	}, nil
}

func parseBitsHex(s string) uint32 {
	var v uint32
	fmt.Sscanf(s, "%x", &v)
	return v
}
