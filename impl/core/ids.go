package core

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"math/big"
	"time"
)

const codeAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"

// newCode produces a short code that is easy to read out and retype.
func newCode() string {
	buf := make([]byte, 8)
	for i := range buf {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(codeAlphabet))))
		if err != nil {
			return fmt.Sprintf("%08d", time.Now().UnixNano()%100000000)
		}
		buf[i] = codeAlphabet[n.Int64()]
	}
	return string(buf[:4]) + "-" + string(buf[4:])
}

// newToken produces an API token for a sender.
func newToken() string {
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("tok%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(buf)
}
