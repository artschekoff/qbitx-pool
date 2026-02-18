package main

import (
	"bufio"
	"context"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"math/big"
	"math/rand"
	"net"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/q-bitx/pool/internal/mining"
)

type stratumMessage struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
	Result json.RawMessage `json:"result"`
	Error  json.RawMessage `json:"error"`
}

type miningJob struct {
	ID           string
	PrevHash     string
	Coinbase1    string
	Coinbase2    string
	MerkleBranch []string
	Version      string
	Bits         string
	NTime        string
}

type clientState struct {
	mu             sync.RWMutex
	extraNonce1    string
	extraNonce2Len int
	difficulty     float64
}

type stateSnapshot struct {
	extraNonce1    string
	extraNonce2Len int
	difficulty     float64
}

func (s *clientState) setSubscribe(extraNonce1 string, extraNonce2Len int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.extraNonce1 = extraNonce1
	s.extraNonce2Len = extraNonce2Len
}

func (s *clientState) setDifficulty(diff float64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.difficulty = diff
}

func (s *clientState) snapshot() stateSnapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return stateSnapshot{
		extraNonce1:    s.extraNonce1,
		extraNonce2Len: s.extraNonce2Len,
		difficulty:     s.difficulty,
	}
}

var diff1Target = func() *big.Int {
	t := new(big.Int)
	t.SetString("00000000ffff0000000000000000000000000000000000000000000000000000", 16)
	return t
}()

func main() {
	if len(os.Args) < 3 {
		fmt.Println("Usage: miner <host:port> <worker>")
		os.Exit(1)
	}

	rand.Seed(time.Now().UnixNano())

	addr := os.Args[1]
	worker := os.Args[2]

	conn, err := net.Dial("tcp", addr)
	if err != nil {
		log.Fatalf("connect: %v", err)
	}
	defer conn.Close()
	log.Printf("Connected to %s", addr)

	reader := bufio.NewReader(conn)
	state := &clientState{difficulty: 1.0}

	var writeMu sync.Mutex
	var nextReqID atomic.Uint64
	nextReqID.Store(2)

	send := func(v interface{}) error {
		writeMu.Lock()
		defer writeMu.Unlock()
		data, err := json.Marshal(v)
		if err != nil {
			return err
		}
		_, err = conn.Write(append(data, '\n'))
		return err
	}

	if err := send(map[string]interface{}{"id": 1, "method": "mining.subscribe", "params": []interface{}{}}); err != nil {
		log.Fatalf("subscribe send: %v", err)
	}
	if err := send(map[string]interface{}{"id": 2, "method": "mining.authorize", "params": []interface{}{worker, "x"}}); err != nil {
		log.Fatalf("authorize send: %v", err)
	}

	jobCh := make(chan miningJob, 1)
	errCh := make(chan error, 1)

	go func() {
		for {
			line, err := reader.ReadBytes('\n')
			if err != nil {
				errCh <- err
				return
			}

			var msg stratumMessage
			if err := json.Unmarshal(line, &msg); err != nil {
				log.Printf("bad message: %v", err)
				continue
			}

			if msg.Method != "" {
				handleNotification(msg, state, jobCh)
				continue
			}

			handleResponse(msg, state)
		}
	}()

	var mineCancel context.CancelFunc
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)

	for {
		select {
		case j := <-jobCh:
			if mineCancel != nil {
				mineCancel()
			}
			ctx, cancel := context.WithCancel(context.Background())
			mineCancel = cancel
			go mineJob(ctx, send, nextReqID.Add(1), worker, state, j)
		case err := <-errCh:
			if mineCancel != nil {
				mineCancel()
			}
			log.Fatalf("read: %v", err)
		case <-sig:
			if mineCancel != nil {
				mineCancel()
			}
			log.Println("Exiting...")
			return
		}
	}
}

func handleNotification(msg stratumMessage, state *clientState, jobCh chan miningJob) {
	switch msg.Method {
	case "mining.set_difficulty":
		var params []float64
		if err := json.Unmarshal(msg.Params, &params); err != nil || len(params) == 0 {
			log.Printf("set_difficulty decode error: %v", err)
			return
		}
		if params[0] <= 0 {
			params[0] = 1.0
		}
		state.setDifficulty(params[0])
		log.Printf("Set difficulty: %.4f", params[0])
	case "mining.notify":
		var params []json.RawMessage
		if err := json.Unmarshal(msg.Params, &params); err != nil || len(params) < 8 {
			log.Printf("notify decode error: %v", err)
			return
		}
		job, err := parseNotify(params)
		if err != nil {
			log.Printf("notify parse error: %v", err)
			return
		}
		select {
		case jobCh <- job:
		default:
			<-jobCh
			jobCh <- job
		}
		log.Printf("New job: %s", job.ID)
	}
}

func handleResponse(msg stratumMessage, state *clientState) {
	if len(msg.Error) > 0 && string(msg.Error) != "null" {
		log.Printf("Server error: %s", string(msg.Error))
		return
	}

	var id int
	if len(msg.ID) == 0 || string(msg.ID) == "null" {
		return
	}
	if err := json.Unmarshal(msg.ID, &id); err != nil {
		return
	}

	switch id {
	case 1:
		var result []json.RawMessage
		if err := json.Unmarshal(msg.Result, &result); err != nil || len(result) < 3 {
			log.Printf("subscribe response parse error: %v", err)
			return
		}
		var en1 string
		if err := json.Unmarshal(result[1], &en1); err != nil {
			log.Printf("subscribe extranonce1 parse error: %v", err)
			return
		}
		en2Len, err := rawToInt(result[2])
		if err != nil {
			log.Printf("subscribe extranonce2 size parse error: %v", err)
			return
		}
		state.setSubscribe(en1, en2Len)
		log.Printf("extranonce1: %s, extranonce2size: %d", en1, en2Len)
	case 2:
		var ok bool
		_ = json.Unmarshal(msg.Result, &ok)
		log.Printf("authorize result: %v", ok)
	default:
		log.Printf("Response: id=%d result=%s", id, string(msg.Result))
	}
}

func parseNotify(params []json.RawMessage) (miningJob, error) {
	var j miningJob
	if err := json.Unmarshal(params[0], &j.ID); err != nil {
		return j, fmt.Errorf("job id: %w", err)
	}
	if err := json.Unmarshal(params[1], &j.PrevHash); err != nil {
		return j, fmt.Errorf("prevhash: %w", err)
	}
	if err := json.Unmarshal(params[2], &j.Coinbase1); err != nil {
		return j, fmt.Errorf("coinb1: %w", err)
	}
	if err := json.Unmarshal(params[3], &j.Coinbase2); err != nil {
		return j, fmt.Errorf("coinb2: %w", err)
	}
	if err := json.Unmarshal(params[4], &j.MerkleBranch); err != nil {
		return j, fmt.Errorf("merkle branch: %w", err)
	}
	if err := json.Unmarshal(params[5], &j.Version); err != nil {
		return j, fmt.Errorf("version: %w", err)
	}
	if err := json.Unmarshal(params[6], &j.Bits); err != nil {
		return j, fmt.Errorf("bits: %w", err)
	}
	if err := json.Unmarshal(params[7], &j.NTime); err != nil {
		return j, fmt.Errorf("ntime: %w", err)
	}
	return j, nil
}

func mineJob(
	ctx context.Context,
	send func(v interface{}) error,
	reqID uint64,
	worker string,
	state *clientState,
	job miningJob,
) {
	s := state.snapshot()
	if s.extraNonce1 == "" || s.extraNonce2Len <= 0 {
		log.Printf("skip job %s: waiting subscribe/extranonce", job.ID)
		return
	}

	prevHash, err := decodeHash32(job.PrevHash)
	if err != nil {
		log.Printf("job %s bad prevhash: %v", job.ID, err)
		return
	}
	cb1, err := hex.DecodeString(job.Coinbase1)
	if err != nil {
		log.Printf("job %s bad coinbase1: %v", job.ID, err)
		return
	}
	cb2, err := hex.DecodeString(job.Coinbase2)
	if err != nil {
		log.Printf("job %s bad coinbase2: %v", job.ID, err)
		return
	}
	en1, err := hex.DecodeString(s.extraNonce1)
	if err != nil {
		log.Printf("job %s bad extranonce1: %v", job.ID, err)
		return
	}

	branch := make([][32]byte, 0, len(job.MerkleBranch))
	for _, h := range job.MerkleBranch {
		h32, err := decodeHash32(h)
		if err != nil {
			log.Printf("job %s bad merkle branch: %v", job.ID, err)
			return
		}
		branch = append(branch, h32)
	}

	versionU, err := strconv.ParseUint(job.Version, 16, 32)
	if err != nil {
		log.Printf("job %s bad version: %v", job.ID, err)
		return
	}
	bitsU, err := strconv.ParseUint(job.Bits, 16, 32)
	if err != nil {
		log.Printf("job %s bad bits: %v", job.ID, err)
		return
	}
	ntimeU, err := strconv.ParseUint(job.NTime, 16, 32)
	if err != nil {
		log.Printf("job %s bad ntime: %v", job.ID, err)
		return
	}

	target := targetFromDifficulty(math.Max(1.0, s.difficulty))
	ntime := uint32(ntimeU)
	version := int32(versionU)
	bits := uint32(bitsU)

	log.Printf("Mining job=%s diff=%.4f", job.ID, s.difficulty)

	hashes := uint64(0)
	start := time.Now()
	lastStat := start

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		en2 := make([]byte, s.extraNonce2Len)
		_, _ = rand.Read(en2)
		en2Hex := hex.EncodeToString(en2)

		coinbase := make([]byte, 0, len(cb1)+len(en1)+len(en2)+len(cb2))
		coinbase = append(coinbase, cb1...)
		coinbase = append(coinbase, en1...)
		coinbase = append(coinbase, en2...)
		coinbase = append(coinbase, cb2...)

		cbHash := mining.DoubleSHA256(coinbase)
		merkleRoot := mining.MerkleRoot(cbHash, branch)

		nonce := rand.Uint32()
		for {
			select {
			case <-ctx.Done():
				return
			default:
			}

			header := mining.BuildBlockHeader(version, prevHash, merkleRoot, ntime, bits, nonce)
			hash := mining.DoubleSHA256(header)
			hashes++

			if hashToBig(hash).Cmp(target) <= 0 {
				nonceHex := make([]byte, 4)
				binary.BigEndian.PutUint32(nonceHex, nonce)
				req := map[string]interface{}{
					"id":     reqID,
					"method": "mining.submit",
					"params": []interface{}{worker, job.ID, en2Hex, job.NTime, hex.EncodeToString(nonceHex)},
				}
				if err := send(req); err != nil {
					log.Printf("submit error: %v", err)
					return
				}
				log.Printf("Found candidate share job=%s nonce=%08x", job.ID, nonce)
				reqID++
				break
			}

			nonce++
			if nonce == 0 {
				break
			}

			now := time.Now()
			if now.Sub(lastStat) >= 5*time.Second {
				rate := float64(hashes) / now.Sub(start).Seconds()
				log.Printf("job=%s hashrate=%.2f H/s", job.ID, rate)
				lastStat = now
			}
		}
	}
}

func rawToInt(raw json.RawMessage) (int, error) {
	var i int
	if err := json.Unmarshal(raw, &i); err == nil {
		return i, nil
	}
	var f float64
	if err := json.Unmarshal(raw, &f); err == nil {
		return int(f), nil
	}
	return 0, fmt.Errorf("not a number: %s", string(raw))
}

func decodeHash32(s string) ([32]byte, error) {
	var h [32]byte
	b, err := hex.DecodeString(s)
	if err != nil {
		return h, err
	}
	if len(b) != 32 {
		return h, fmt.Errorf("expected 32 bytes, got %d", len(b))
	}
	copy(h[:], b)
	return h, nil
}

func hashToBig(h [32]byte) *big.Int {
	var buf [32]byte
	copy(buf[:], h[:])
	mining.ReverseBytes(buf[:])
	return new(big.Int).SetBytes(buf[:])
}

func targetFromDifficulty(diff float64) *big.Int {
	if diff <= 0 {
		diff = 1
	}
	d := new(big.Float).SetInt(diff1Target)
	denom := big.NewFloat(diff)
	t := new(big.Float).Quo(d, denom)
	out := new(big.Int)
	t.Int(out)
	if out.Sign() <= 0 {
		return big.NewInt(1)
	}
	return out
}
