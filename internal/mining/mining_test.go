package mining

import (
	"encoding/binary"
	"encoding/hex"
	"testing"
)

func TestCalcDeveloperFee(t *testing.T) {
	if got := calcDeveloperFee(1250000000, 0); got != 0 {
		t.Fatalf("expected zero fee, got %d", got)
	}
	if got := calcDeveloperFee(1250000000, 1.5); got != 18750000 {
		t.Fatalf("expected 18750000, got %d", got)
	}
	if got := calcDeveloperFee(1000, 99.99); got <= 0 || got >= 1000 {
		t.Fatalf("expected fee in range (0,1000), got %d", got)
	}
}

func TestBuildCoinbaseTx_DeveloperFeeOutput(t *testing.T) {
	_, cb2NoFeeHex := BuildCoinbaseTx(100, 1250000000, "MWnxrbCdBdtUP2M5NZ7v5bYrbZ78ZsMBzh", "QBXPOOL", "", 0, "")
	cb2NoFee, err := hex.DecodeString(cb2NoFeeHex)
	if err != nil {
		t.Fatalf("decode cb2 no fee: %v", err)
	}
	if len(cb2NoFee) < 5 || cb2NoFee[4] != 1 {
		t.Fatalf("expected 1 output without fee, got %d", cb2NoFee[4])
	}

	_, cb2FeeHex := BuildCoinbaseTx(100, 1250000000, "MWnxrbCdBdtUP2M5NZ7v5bYrbZ78ZsMBzh", "QBXPOOL", "", 1.5, "MWnxrbCdBdtUP2M5NZ7v5bYrbZ78ZsMBzh")
	cb2Fee, err := hex.DecodeString(cb2FeeHex)
	if err != nil {
		t.Fatalf("decode cb2 fee: %v", err)
	}
	if len(cb2Fee) < 5 || cb2Fee[4] != 2 {
		t.Fatalf("expected 2 outputs with fee, got %d", cb2Fee[4])
	}

	values := parseOutputValues(t, cb2Fee)
	if len(values) != 2 {
		t.Fatalf("expected 2 outputs, got %d", len(values))
	}
	if values[0]+values[1] != 1250000000 {
		t.Fatalf("unexpected output sum: %d", values[0]+values[1])
	}
	if values[1] != 18750000 {
		t.Fatalf("unexpected developer fee value: %d", values[1])
	}
}

func parseOutputValues(t *testing.T, cb2 []byte) []int64 {
	t.Helper()
	if len(cb2) < 5 {
		t.Fatalf("coinbase2 too short")
	}
	count := int(cb2[4])
	out := make([]int64, 0, count)
	i := 5
	for n := 0; n < count; n++ {
		if i+8 > len(cb2) {
			t.Fatalf("short value for output %d", n)
		}
		v := int64(binary.LittleEndian.Uint64(cb2[i : i+8]))
		i += 8
		if i >= len(cb2) {
			t.Fatalf("short script length for output %d", n)
		}
		scriptLen := int(cb2[i])
		i++
		if i+scriptLen > len(cb2) {
			t.Fatalf("short script for output %d", n)
		}
		i += scriptLen
		out = append(out, v)
	}
	return out
}
