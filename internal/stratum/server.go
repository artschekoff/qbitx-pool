package stratum

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/q-bitx/pool/internal/accounting"
	"github.com/q-bitx/pool/internal/config"
	"github.com/q-bitx/pool/internal/job"
	"github.com/q-bitx/pool/internal/share"
)

type Request struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

type Response struct {
	ID     json.RawMessage `json:"id"`
	Result interface{}     `json:"result"`
	Error  interface{}     `json:"error"`
}

type Notification struct {
	ID     interface{} `json:"id"`
	Method string      `json:"method"`
	Params interface{} `json:"params"`
}

type Server struct {
	cfg       *config.Config
	jm        *job.Maker
	validator validator
	store     accounting.ShareStore
	nextEN1   atomic.Uint32
	connsMu   sync.Mutex
	conns     map[uint32]*Conn
}

type validator interface {
	Validate(sub share.Submission) share.Result
}

func NewServer(cfg *config.Config, jm *job.Maker, v validator, store accounting.ShareStore) *Server {
	return &Server{cfg: cfg, jm: jm, validator: v, store: store, conns: make(map[uint32]*Conn)}
}

func (s *Server) ListenAndServe() error {
	ln, err := net.Listen("tcp", s.cfg.Stratum.Listen)
	if err != nil {
		return fmt.Errorf("stratum listen: %w", err)
	}
	log.Printf("[stratum] listening on %s", s.cfg.Stratum.Listen)
	jobCh := s.jm.Subscribe()
	go func() {
		for j := range jobCh {
			s.broadcastJob(j)
		}
	}()
	for {
		c, err := ln.Accept()
		if err != nil {
			log.Printf("[stratum] accept: %v", err)
			continue
		}
		go s.handleConn(c)
	}
}

type Conn struct {
	conn       net.Conn
	server     *Server
	en1        uint32
	en1Hex     string
	worker     string
	authorized bool
	diff       float64
	currentJob string
	submitted  map[string]struct{}
	acceptedAt []time.Time
	lastAdjust time.Time
	mu         sync.Mutex
}

func (s *Server) handleConn(c net.Conn) {
	en1 := s.nextEN1.Add(1)
	en1Hex := fmt.Sprintf("%08x", en1)
	conn := &Conn{
		conn:      c,
		server:    s,
		en1:       en1,
		en1Hex:    en1Hex,
		diff:      s.cfg.Difficulty.Start,
		submitted: make(map[string]struct{}),
	}
	s.connsMu.Lock()
	s.conns[en1] = conn
	s.connsMu.Unlock()
	defer func() {
		c.Close()
		s.connsMu.Lock()
		delete(s.conns, en1)
		s.connsMu.Unlock()
	}()
	reader := bufio.NewReader(c)
	for {
		_ = c.SetReadDeadline(time.Now().Add(5 * time.Minute))
		line, err := reader.ReadBytes('\n')
		if err != nil {
			return
		}
		var req Request
		if err := json.Unmarshal(line, &req); err != nil {
			log.Printf("[stratum] bad json from %s: %v", c.RemoteAddr(), err)
			continue
		}
		conn.handleRequest(&req)
	}
}

func (cn *Conn) handleRequest(req *Request) {
	switch req.Method {
	case "mining.configure":
		cn.handleConfigure(req)
	case "mining.subscribe":
		cn.handleSubscribe(req)
	case "mining.authorize":
		cn.handleAuthorize(req)
	case "mining.submit":
		cn.handleSubmit(req)
	case "mining.extranonce.subscribe":
		cn.sendResult(req.ID, true)
	default:
		cn.sendError(req.ID, 20, "unknown method")
	}
}

func (cn *Conn) handleConfigure(req *Request) {
	var params []json.RawMessage
	if len(req.Params) > 0 {
		if err := json.Unmarshal(req.Params, &params); err != nil {
			cn.sendError(req.ID, 20, "bad params")
			return
		}
	}

	requested := []string{}
	if len(params) > 0 {
		_ = json.Unmarshal(params[0], &requested)
	}

	result := map[string]interface{}{}
	for _, ext := range requested {
		switch ext {
		case "subscribe-extranonce":
			result[ext] = true
		case "version-rolling":
			result[ext] = false
		case "minimum-difficulty":
			result[ext] = false
		default:
			result[ext] = false
		}
	}
	if len(result) == 0 {
		result["subscribe-extranonce"] = true
	}
	cn.sendResult(req.ID, result)
}

func (cn *Conn) handleSubscribe(req *Request) {
	result := []interface{}{
		[][]string{{"mining.set_difficulty", "1"}, {"mining.notify", "1"}},
		cn.en1Hex,
		job.ExtraNonce2Size,
	}
	cn.sendResult(req.ID, result)
	cn.sendNotification("mining.set_difficulty", []interface{}{cn.diff})
	if j := cn.server.jm.Current(); j != nil {
		cn.sendJob(j)
	}
}

func (cn *Conn) handleAuthorize(req *Request) {
	var params []string
	if err := json.Unmarshal(req.Params, &params); err != nil || len(params) == 0 {
		cn.sendError(req.ID, 20, "bad params")
		return
	}
	cn.mu.Lock()
	cn.worker = params[0]
	cn.authorized = true
	cn.diff = cn.pickStartDiff(params[0])
	cn.mu.Unlock()
	log.Printf("[stratum] authorized: %s from %s", params[0], cn.conn.RemoteAddr())
	cn.sendResult(req.ID, true)
	cn.sendNotification("mining.set_difficulty", []interface{}{cn.diff})
}

func (cn *Conn) handleSubmit(req *Request) {
	var params []string
	if err := json.Unmarshal(req.Params, &params); err != nil || len(params) < 5 {
		cn.sendError(req.ID, 20, "bad params")
		return
	}
	cn.mu.Lock()
	auth := cn.authorized
	worker := cn.worker
	currentJob := cn.currentJob
	cn.mu.Unlock()
	if !auth {
		cn.sendError(req.ID, 24, "unauthorized")
		return
	}
	if currentJob == "" || params[1] != currentJob {
		cn.sendError(req.ID, 21, "stale job")
		return
	}
	submissionKey := fmt.Sprintf("%s|%s|%s|%s", params[1], params[2], params[3], params[4])
	if !cn.markSubmitted(submissionKey) {
		cn.sendError(req.ID, 22, "duplicate share")
		return
	}

	result := cn.server.validator.Validate(share.Submission{
		ExtraNonce1: cn.en1Hex,
		ExtraNonce2: params[2],
		NTime:       params[3],
		Nonce:       params[4],
	})
	if result.Err != nil {
		log.Printf("[stratum] reject from %s: %v", worker, result.Err)
		cn.sendError(req.ID, 23, result.Err.Error())
		return
	}
	if result.IsBlock {
		log.Printf("[stratum] *** BLOCK FOUND *** height=%d by %s hash=%s", result.Height, worker, result.Hash)
	} else {
		log.Printf("[stratum] share ok from %s diff=%.2f", worker, result.Diff)
	}
	now := time.Now().UTC()
	entryType := "share"
	if result.IsBlock {
		entryType = "block"
	}
	err := cn.server.store.InsertAcceptedShare(context.Background(), accounting.ShareRecord{
		Worker:        worker,
		Diff:          result.Diff,
		Time:          now,
		Type:          entryType,
		SubmissionKey: submissionKey,
	})
	if err != nil {
		log.Printf("[stratum] share accounting failed worker=%s: %v", worker, err)
		cn.sendError(req.ID, 23, "share accounting unavailable")
		return
	}
	cn.sendResult(req.ID, true)
	if oldDiff, newDiff, changed := cn.retarget(now); changed {
		log.Printf("[stratum] vardiff worker=%s %.2f -> %.2f", worker, oldDiff, newDiff)
		cn.sendNotification("mining.set_difficulty", []interface{}{newDiff})
	}
}

func (cn *Conn) sendJob(j *job.Job) {
	cn.mu.Lock()
	cn.currentJob = j.ID
	cn.submitted = make(map[string]struct{})
	cn.mu.Unlock()

	params := []interface{}{
		j.ID, j.PrevHash, j.Coinbase1, j.Coinbase2,
		j.MerkleBranch, j.Version, j.Bits, j.Time, j.CleanJobs,
	}
	cn.sendNotification("mining.notify", params)
}

func (cn *Conn) markSubmitted(key string) bool {
	cn.mu.Lock()
	defer cn.mu.Unlock()
	if _, ok := cn.submitted[key]; ok {
		return false
	}
	cn.submitted[key] = struct{}{}
	return true
}

func (cn *Conn) retarget(now time.Time) (float64, float64, bool) {
	cfg := cn.server.cfg.Difficulty
	target := float64(cfg.TargetTime)
	if target <= 0 {
		target = 10
	}
	interval := time.Duration(cfg.RetargetInterval) * time.Second
	if interval <= 0 {
		interval = 30 * time.Second
	}

	cn.mu.Lock()
	defer cn.mu.Unlock()

	if now.IsZero() {
		now = time.Now()
	}
	cn.acceptedAt = append(cn.acceptedAt, now)
	if cn.lastAdjust.IsZero() {
		cn.lastAdjust = now
		return 0, 0, false
	}
	if now.Sub(cn.lastAdjust) < interval {
		return 0, 0, false
	}

	cutoff := now.Add(-interval)
	dst := cn.acceptedAt[:0]
	for _, t := range cn.acceptedAt {
		if t.After(cutoff) {
			dst = append(dst, t)
		}
	}
	cn.acceptedAt = dst
	if len(cn.acceptedAt) == 0 {
		cn.lastAdjust = now
		return 0, 0, false
	}

	actual := interval.Seconds() / float64(len(cn.acceptedAt))
	if actual <= 0 {
		cn.lastAdjust = now
		return 0, 0, false
	}
	factor := target / actual
	if factor > 4 {
		factor = 4
	}
	if factor < 0.25 {
		factor = 0.25
	}
	newDiff := clampDiff(cn.diff*factor, cfg.Min, cfg.Max)
	oldDiff := cn.diff
	cn.lastAdjust = now
	cn.acceptedAt = cn.acceptedAt[:0]
	if oldDiff <= 0 {
		oldDiff = 1
	}
	if math.Abs(newDiff-oldDiff)/oldDiff < 0.10 {
		return 0, 0, false
	}
	cn.diff = newDiff
	return oldDiff, newDiff, true
}

func clampDiff(v float64, min float64, max float64) float64 {
	if v <= 0 {
		v = 1
	}
	if min > 0 && v < min {
		v = min
	}
	if max > 0 && v > max {
		v = max
	}
	return v
}

func (cn *Conn) pickStartDiff(worker string) float64 {
	cfg := cn.server.cfg.Difficulty
	w := strings.ToLower(worker)
	switch {
	case strings.Contains(w, "gpu"):
		if cfg.GPUStart > 0 {
			return clampDiff(cfg.GPUStart, cfg.Min, cfg.Max)
		}
		return clampDiff(10, cfg.Min, cfg.Max)
	case strings.Contains(w, "asic"):
		if cfg.ASICStart > 0 {
			return clampDiff(cfg.ASICStart, cfg.Min, cfg.Max)
		}
		return clampDiff(cfg.Start, cfg.Min, cfg.Max)
	default:
		return clampDiff(cfg.Start, cfg.Min, cfg.Max)
	}
}

func (s *Server) broadcastJob(j *job.Job) {
	s.connsMu.Lock()
	list := make([]*Conn, 0, len(s.conns))
	for _, c := range s.conns {
		list = append(list, c)
	}
	s.connsMu.Unlock()
	for _, c := range list {
		c.sendJob(j)
	}
}

func (cn *Conn) sendResult(id json.RawMessage, result interface{}) {
	cn.send(Response{ID: id, Result: result, Error: nil})
}

func (cn *Conn) sendError(id json.RawMessage, code int, msg string) {
	cn.send(Response{ID: id, Result: nil, Error: []interface{}{code, msg, nil}})
}

func (cn *Conn) sendNotification(method string, params interface{}) {
	cn.send(Notification{ID: nil, Method: method, Params: params})
}

func (cn *Conn) send(v interface{}) {
	data, err := json.Marshal(v)
	if err != nil {
		return
	}
	data = append(data, '\n')
	cn.mu.Lock()
	defer cn.mu.Unlock()
	_ = cn.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	_, _ = cn.conn.Write(data)
}
