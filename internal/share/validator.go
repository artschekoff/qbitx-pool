package share

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"log"
	"math/big"

	"github.com/q-bitx/pool/internal/daemon"
	"github.com/q-bitx/pool/internal/job"
	"github.com/q-bitx/pool/internal/mining"
)

// Submission holds the miner-provided share fields.
type Submission struct {
	ExtraNonce1 string
	ExtraNonce2 string
	NTime       string
	Nonce       string
}

// Result describes the outcome of a share check.
type Result struct {
	Err     error
	IsBlock bool
	Hash    string
	Diff    float64
	Height  int64
}

// Validator checks submitted shares against the current job.
type Validator struct {
	jm     *job.Maker
	client *daemon.Client
}

// NewValidator creates a share validator.
func NewValidator(jm *job.Maker, client *daemon.Client) *Validator {
	return &Validator{jm: jm, client: client}
}

// Validate checks a submitted share.
func (v *Validator) Validate(sub Submission) Result {
	j := v.jm.Current()
	if j == nil {
		return Result{Err: fmt.Errorf("no current job")}
	}

	// 1. rebuild coinbase
	en1, err := hex.DecodeString(sub.ExtraNonce1)
	if err != nil || len(en1) != job.ExtraNonce1Size {
		return Result{Err: fmt.Errorf("bad extranonce1")}
	}
	en2, err := hex.DecodeString(sub.ExtraNonce2)
	if err != nil || len(en2) != job.ExtraNonce2Size {
		return Result{Err: fmt.Errorf("bad extranonce2")}
	}
	cb1, err := hex.DecodeString(j.Coinbase1)
	if err != nil {
		return Result{Err: fmt.Errorf("bad coinbase1 hex")}
	}
	cb2, err := hex.DecodeString(j.Coinbase2)
	if err != nil {
		return Result{Err: fmt.Errorf("bad coinbase2 hex")}
	}

	coinbaseTx := make([]byte, 0, len(cb1)+len(en1)+len(en2)+len(cb2))
	coinbaseTx = append(coinbaseTx, cb1...)
	coinbaseTx = append(coinbaseTx, en1...)
	coinbaseTx = append(coinbaseTx, en2...)
	coinbaseTx = append(coinbaseTx, cb2...)

	// 2. coinbase hash
	cbHash := mining.DoubleSHA256(coinbaseTx)

	// 3. merkle root
	branchBytes := make([][32]byte, len(j.MerkleBranch))
	for i, s := range j.MerkleBranch {
		b, _ := hex.DecodeString(s)
		copy(branchBytes[i][:], b)
	}
	merkleRoot := mining.MerkleRoot(cbHash, branchBytes)

	// 4. parse ntime and nonce
	ntime, err := parseHex32(sub.NTime)
	if err != nil {
		return Result{Err: fmt.Errorf("bad ntime: %w", err)}
	}
	nonce, err := parseHex32(sub.Nonce)
	if err != nil {
		return Result{Err: fmt.Errorf("bad nonce: %w", err)}
	}

	// 5. build header and hash
	header := mining.BuildBlockHeader(j.VersionInt, j.PrevHashBytes, merkleRoot, ntime, j.BitsUint, nonce)
	headerHash := mining.DoubleSHA256(header)
	hashStr := mining.HashToString(headerHash)

	// 6. compare with target
	hashBig := hashToBig(headerHash)
	targetBig := targetFromBits(j.BitsUint)
	diff := diffFromHash(hashBig)

	if hashBig.Cmp(targetBig) <= 0 {
		log.Printf("[share] BLOCK FOUND height=%d, submitting...", j.Height)
		blockHex := buildBlockHex(header, coinbaseTx, j)
		if err := v.client.SubmitBlock(blockHex); err != nil {
			log.Printf("[share] submitblock error: %v", err)
		}
		return Result{IsBlock: true, Hash: hashStr, Diff: diff, Height: j.Height}
	}

	if diff < 1.0 {
		return Result{Err: fmt.Errorf("share below minimum difficulty (%.4f)", diff)}
	}
	return Result{Hash: hashStr, Diff: diff, Height: j.Height}
}

// ---------------------------------------------------------------------------

func parseHex32(s string) (uint32, error) {
	b, err := hex.DecodeString(s)
	if err != nil || len(b) != 4 {
		return 0, fmt.Errorf("expected 4 hex bytes")
	}
	return binary.BigEndian.Uint32(b), nil
}

func hashToBig(h [32]byte) *big.Int {
	var buf [32]byte
	copy(buf[:], h[:])
	mining.ReverseBytes(buf[:])
	return new(big.Int).SetBytes(buf[:])
}

func targetFromBits(bits uint32) *big.Int {
	exp := bits >> 24
	man := bits & 0x007fffff
	if exp <= 3 {
		man >>= 8 * (3 - exp)
		return big.NewInt(int64(man))
	}
	t := big.NewInt(int64(man))
	t.Lsh(t, uint(8*(exp-3)))
	return t
}

var diff1Target = func() *big.Int {
	t := new(big.Int)
	t.SetString("00000000ffff0000000000000000000000000000000000000000000000000000", 16)
	return t
}()

func diffFromHash(h *big.Int) float64 {
	if h.Sign() == 0 {
		return 0
	}
	d := new(big.Float).SetInt(diff1Target)
	hf := new(big.Float).SetInt(h)
	r := new(big.Float).Quo(d, hf)
	f, _ := r.Float64()
	return f
}

func buildBlockHex(header []byte, coinbaseTx []byte, j *job.Job) string {
	var buf []byte
	buf = append(buf, header...)
	txCount := 1 + len(j.RawTxData)
	buf = append(buf, encodeVarInt(uint64(txCount))...)
	buf = append(buf, coinbaseTx...)
	for _, raw := range j.RawTxData {
		buf = append(buf, raw...)
	}
	return hex.EncodeToString(buf)
}

func encodeVarInt(v uint64) []byte {
	switch {
	case v < 0xfd:
		return []byte{byte(v)}
	case v <= 0xffff:
		b := make([]byte, 3)
		b[0] = 0xfd
		binary.LittleEndian.PutUint16(b[1:], uint16(v))
		return b
	case v <= 0xffffffff:
		b := make([]byte, 5)
		b[0] = 0xfe
		binary.LittleEndian.PutUint32(b[1:], uint32(v))
		return b
	default:
		b := make([]byte, 9)
		b[0] = 0xff
		binary.LittleEndian.PutUint64(b[1:], v)
		return b
	}
}
