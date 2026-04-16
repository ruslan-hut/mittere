package telegram

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"strings"
)

func removeMarkup(input string) string {
	reservedChars := "\\`*_|"

	sanitized := ""
	for _, char := range input {
		if !strings.ContainsRune(reservedChars, char) {
			sanitized += string(char)
		}
	}

	return sanitized
}

func extractCommand(text string) string {
	if len(text) == 0 || text[0] != '/' {
		return ""
	}
	cmd := text[1:]
	if i := strings.IndexAny(cmd, " @"); i != -1 {
		cmd = cmd[:i]
	}
	return cmd
}

func generatePinCode() string {
	n, err := rand.Int(rand.Reader, big.NewInt(1000000))
	if err != nil {
		return "000000"
	}
	return fmt.Sprintf("%06d", n.Int64())
}
