package telegram

import (
	"regexp"
	"strings"
	"unicode"
)

var codePattern = regexp.MustCompile(`^[A-Z0-9]{4}-[A-Z0-9]{4}$`)

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

// escapeText escapes MarkdownV2 reserved symbols (models.ParseModeMarkdown is MarkdownV2),
// keeping '*' so senders can mark bold text.
func escapeText(input string) string {
	var sb strings.Builder
	for _, char := range input {
		if strings.ContainsRune("\\_[]()~`>#+-=|{}.!", char) {
			sb.WriteRune('\\')
		}
		sb.WriteRune(char)
	}
	return sb.String()
}

// escapeCode escapes symbols that MarkdownV2 reserves inside pre and code entities.
func escapeCode(input string) string {
	var sb strings.Builder
	for _, char := range input {
		if char == '\\' || char == '`' {
			sb.WriteRune('\\')
		}
		sb.WriteRune(char)
	}
	return sb.String()
}

// extractCommand splits a message into a bot command and its arguments. The
// bot mention is cut off, so the same command works in a group, where commands
// are addressed as /link@some_bot.
func extractCommand(text string) (string, string) {
	text = strings.TrimSpace(text)
	if len(text) == 0 || text[0] != '/' {
		return "", ""
	}
	cmd := text[1:]
	args := ""
	if i := strings.IndexAny(cmd, " \n"); i != -1 {
		args = strings.TrimSpace(cmd[i+1:])
		cmd = cmd[:i]
	}
	if i := strings.Index(cmd, "@"); i != -1 {
		cmd = cmd[:i]
	}
	return strings.ToLower(cmd), args
}

// looksLikeCode reports whether a plain message is a linking code.
func looksLikeCode(text string) bool {
	return codePattern.MatchString(strings.ToUpper(strings.TrimSpace(text)))
}

// hashtag keeps only letters and digits of a subject, joining the words with
// underscores, so Telegram treats the whole subject as one tag.
func hashtag(subject string) string {
	words := strings.FieldsFunc(subject, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	return strings.Join(words, "_")
}
