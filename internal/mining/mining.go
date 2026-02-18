package mining

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
)

// DoubleSHA256 computes SHA256(SHA256(data)).
func DoubleSHA256(data []byte) [32]byte {
	first := sha256.Sum256(data)
	return sha256.Sum256(first[:])
}

// HashToString returns a little-endian hex string (block hash display order).
func HashToString(h [32]byte) string {
	for i, j := 0, len(h)-1; i < j; i, j = i+1, j-1 {
		h[i], h[j] = h[j], h[i]
	}
	return hex.EncodeToString(h[:])
}

// ReverseBytes reverses a byte slice in place.
func ReverseBytes(b []byte) {
	for i, j := 0, len(b)-1; i < j; i, j = i+1, j-1 {
		b[i], b[j] = b[j], b[i]
	}
}

// BuildCoinbaseTx builds a serialised coinbase transaction split into two
// halves (before extranonce, after extranonce) so the stratum server can
// insert extranonce1 + extranonce2 in between.
//
// Returns (coinbase1_hex, coinbase2_hex).
func BuildCoinbaseTx(height int64, coinbaseValue int64, address string, tag string, payoutSPKHex string) (string, string) {
	heightBytes := encodeHeight(height)
	tagBytes := []byte(tag)
	scriptSigPrefix := append(heightBytes, tagBytes...)

	var cb1 []byte
	// version
	cb1 = appendLE32(cb1, 1)
	// input count
	cb1 = append(cb1, 0x01)
	// prev out hash (32 zero bytes)
	cb1 = append(cb1, make([]byte, 32)...)
	// prev out index 0xffffffff
	cb1 = appendLE32(cb1, 0xffffffff)
	// scriptSig length = prefix + extranonce1(4) + extranonce2(4)
	scriptSigLen := len(scriptSigPrefix) + 4 + 4
	cb1 = append(cb1, encodeVarInt(uint64(scriptSigLen))...)
	cb1 = append(cb1, scriptSigPrefix...)

	var cb2 []byte
	// sequence
	cb2 = appendLE32(cb2, 0xffffffff)
	// output count
	cb2 = append(cb2, 0x01)
	// value (8 bytes LE)
	cb2 = appendLE64(cb2, coinbaseValue)
	// output script
	var pkScript []byte
	if payoutSPKHex != "" {
		pkScript, _ = hex.DecodeString(payoutSPKHex)
	}
	if len(pkScript) == 0 {
		pkScript = buildPayoutScript(address)
	}
	cb2 = append(cb2, encodeVarInt(uint64(len(pkScript)))...)
	cb2 = append(cb2, pkScript...)
	// locktime
	cb2 = appendLE32(cb2, 0)

	return hex.EncodeToString(cb1), hex.EncodeToString(cb2)
}

// buildPayoutScript creates a P2PKH script for a base58check address.
func buildPayoutScript(address string) []byte {
	decoded := base58Decode(address)
	if decoded == nil || len(decoded) < 21 {
		return []byte{0x6a, 0x04, 'p', 'o', 'o', 'l'}
	}
	hash160 := decoded[1:21]
	script := []byte{0x76, 0xa9, 0x14}
	script = append(script, hash160...)
	script = append(script, 0x88, 0xac)
	return script
}

// base58Decode is a minimal base58check decoder.
func base58Decode(s string) []byte {
	const alphabet = "123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz"
	result := make([]byte, 0, len(s))
	for _, c := range []byte(s) {
		carry := -1
		for i, a := range []byte(alphabet) {
			if a == c {
				carry = i
				break
			}
		}
		if carry < 0 {
			return nil
		}
		for i := range result {
			carry += int(result[i]) * 58
			result[i] = byte(carry & 0xff)
			carry >>= 8
		}
		for carry > 0 {
			result = append(result, byte(carry&0xff))
			carry >>= 8
		}
	}
	for _, c := range []byte(s) {
		if c != '1' {
			break
		}
		result = append(result, 0)
	}
	for i, j := 0, len(result)-1; i < j; i, j = i+1, j-1 {
		result[i], result[j] = result[j], result[i]
	}
	if len(result) < 4 {
		return nil
	}
	return result[:len(result)-4]
}

// encodeHeight builds BIP 34 height push for coinbase scriptSig.
func encodeHeight(h int64) []byte {
	if h <= 16 {
		return []byte{byte(0x50 + h)}
	}
	buf := make([]byte, 0, 5)
	v := h
	for v > 0 {
		buf = append(buf, byte(v&0xff))
		v >>= 8
	}
	if buf[len(buf)-1]&0x80 != 0 {
		buf = append(buf, 0)
	}
	return append([]byte{byte(len(buf))}, buf...)
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

func appendLE32(dst []byte, v uint32) []byte {
	var buf [4]byte
	binary.LittleEndian.PutUint32(buf[:], v)
	return append(dst, buf[:]...)
}

func appendLE64(dst []byte, v int64) []byte {
	var buf [8]byte
	binary.LittleEndian.PutUint64(buf[:], uint64(v))
	return append(dst, buf[:]...)
}

// MerkleBranch computes the merkle branch for the coinbase (index 0).
func MerkleBranch(txHashes [][32]byte) [][32]byte {
	var branch [][32]byte
	if len(txHashes) == 0 {
		return branch
	}
	current := make([][32]byte, len(txHashes))
	copy(current, txHashes)
	for len(current) > 0 {
		branch = append(branch, current[0])
		var next [][32]byte
		for i := 0; i < len(current); i += 2 {
			var left, right [32]byte
			left = current[i]
			if i+1 < len(current) {
				right = current[i+1]
			} else {
				right = left
			}
			combined := append(left[:], right[:]...)
			next = append(next, DoubleSHA256(combined))
		}
		current = next
		if len(current) == 1 {
			branch = append(branch, current[0])
			break
		}
	}
	return branch
}

// MerkleRoot computes the final merkle root given the coinbase hash and branch.
func MerkleRoot(coinbaseHash [32]byte, branch [][32]byte) [32]byte {
	current := coinbaseHash
	for _, b := range branch {
		combined := append(current[:], b[:]...)
		current = DoubleSHA256(combined)
	}
	return current
}

// BuildBlockHeader builds an 80-byte block header.
func BuildBlockHeader(version int32, prevHash [32]byte, merkleRoot [32]byte, timestamp uint32, bits uint32, nonce uint32) []byte {
	hdr := make([]byte, 80)
	binary.LittleEndian.PutUint32(hdr[0:4], uint32(version))
	copy(hdr[4:36], prevHash[:])
	copy(hdr[36:68], merkleRoot[:])
	binary.LittleEndian.PutUint32(hdr[68:72], timestamp)
	binary.LittleEndian.PutUint32(hdr[72:76], bits)
	binary.LittleEndian.PutUint32(hdr[76:80], nonce)
	return hdr
}

// HexToHash32 converts a 64-char hex string to [32]byte.
func HexToHash32(s string) ([32]byte, error) {
	var h [32]byte
	b, err := hex.DecodeString(s)
	if err != nil {
		return h, fmt.Errorf("hex decode: %w", err)
	}
	if len(b) != 32 {
		return h, fmt.Errorf("expected 32 bytes, got %d", len(b))
	}
	copy(h[:], b)
	return h, nil
}

// HexToHash32Rev converts a display-order (reversed) hash hex to internal order.
func HexToHash32Rev(s string) ([32]byte, error) {
	h, err := HexToHash32(s)
	if err != nil {
		return h, err
	}
	ReverseBytes(h[:])
	return h, nil
}
