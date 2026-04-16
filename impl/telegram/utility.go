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

func sanitize(input string) string {
	reservedChars := "\\`*_{}[]()#+-.!|="

	sanitized := ""
	for _, char := range input {
		if strings.ContainsRune(reservedChars, char) {
			sanitized += "\\" + string(char)
		} else {
			sanitized += string(char)
		}
	}

	return sanitized
}

func generatePinCode() string {
	n, err := rand.Int(rand.Reader, big.NewInt(1000000))
	if err != nil {
		return "000000"
	}
	return fmt.Sprintf("%06d", n.Int64())
}
