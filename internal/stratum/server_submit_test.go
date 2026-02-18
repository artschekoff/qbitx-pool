package stratum

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/q-bitx/pool/internal/accounting"
	"github.com/q-bitx/pool/internal/config"
	"github.com/q-bitx/pool/internal/share"
)

type stubValidator struct {
	result share.Result
}

func (s stubValidator) Validate(_ share.Submission) share.Result {
	return s.result
}

type stubStore struct {
	err  error
	seen []accounting.ShareRecord
}

func (s *stubStore) InsertAcceptedShare(_ context.Context, r accounting.ShareRecord) error {
	s.seen = append(s.seen, r)
	return s.err
}

func (s *stubStore) Close() {}

type memConn struct {
	buf bytes.Buffer
}

func (m *memConn) Read(_ []byte) (int, error)         { return 0, io.EOF }
func (m *memConn) Write(p []byte) (int, error)        { return m.buf.Write(p) }
func (m *memConn) Close() error                       { return nil }
func (m *memConn) LocalAddr() net.Addr                { return stubAddr("local") }
func (m *memConn) RemoteAddr() net.Addr               { return stubAddr("remote") }
func (m *memConn) SetDeadline(_ time.Time) error      { return nil }
func (m *memConn) SetReadDeadline(_ time.Time) error  { return nil }
func (m *memConn) SetWriteDeadline(_ time.Time) error { return nil }

type stubAddr string

func (s stubAddr) Network() string { return "tcp" }
func (s stubAddr) String() string  { return string(s) }

func TestHandleSubmitRejectsWhenAccountingFails(t *testing.T) {
	cfg := &config.Config{Difficulty: config.DifficultyCfg{Start: 1}}
	store := &stubStore{err: io.ErrUnexpectedEOF}
	srv := NewServer(cfg, nil, stubValidator{result: share.Result{Diff: 10}}, store)
	conn := &Conn{
		conn:       &memConn{},
		server:     srv,
		en1Hex:     "00000001",
		worker:     "worker.001",
		authorized: true,
		currentJob: "job1",
		submitted:  map[string]struct{}{},
	}

	params, _ := json.Marshal([]string{"worker.001", "job1", "00000002", "67b493f0", "00000001"})
	req := &Request{ID: json.RawMessage("1"), Method: "mining.submit", Params: params}
	conn.handleSubmit(req)

	out := conn.conn.(*memConn).buf.String()
	if !strings.Contains(out, `"result":null`) || !strings.Contains(out, "share accounting unavailable") {
		t.Fatalf("expected accounting reject response, got %s", out)
	}
	if len(store.seen) != 1 {
		t.Fatalf("expected one insert attempt, got %d", len(store.seen))
	}
}

func TestHandleSubmitAcceptsWhenAccountingSucceeds(t *testing.T) {
	cfg := &config.Config{Difficulty: config.DifficultyCfg{Start: 1}}
	store := &stubStore{}
	srv := NewServer(cfg, nil, stubValidator{result: share.Result{Diff: 22.5}}, store)
	conn := &Conn{
		conn:       &memConn{},
		server:     srv,
		en1Hex:     "00000001",
		worker:     "worker.001",
		authorized: true,
		currentJob: "job1",
		submitted:  map[string]struct{}{},
	}

	params, _ := json.Marshal([]string{"worker.001", "job1", "00000002", "67b493f0", "00000001"})
	req := &Request{ID: json.RawMessage("1"), Method: "mining.submit", Params: params}
	conn.handleSubmit(req)

	out := conn.conn.(*memConn).buf.String()
	if !strings.Contains(out, `"result":true`) {
		t.Fatalf("expected accept response, got %s", out)
	}
	if len(store.seen) != 1 {
		t.Fatalf("expected one insert, got %d", len(store.seen))
	}
	if store.seen[0].Type != "share" {
		t.Fatalf("expected share type, got %s", store.seen[0].Type)
	}
}
