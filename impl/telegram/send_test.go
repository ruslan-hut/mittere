package telegram

import (
	"errors"
	"fmt"
	"github.com/go-telegram/bot"
	"testing"
)

// TestIsMarkupError decides which failures are worth a second, plain text
// attempt. Repeating the others costs a call and can double a message Telegram
// has already accepted.
func TestIsMarkupError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "markdown the parser rejects",
			err:  fmt.Errorf("%w, Bad Request: can't parse entities: Character '-' is reserved", bot.ErrorBadRequest),
			want: true,
		},
		{
			name: "bot blocked by the user",
			err:  fmt.Errorf("%w, Forbidden: bot was blocked by the user", bot.ErrorForbidden),
		},
		{
			name: "bot kicked from the group",
			err:  fmt.Errorf("%w, Forbidden: bot was kicked from the supergroup chat", bot.ErrorForbidden),
		},
		{
			name: "chat not found",
			err:  fmt.Errorf("%w, Bad Request: chat not found", bot.ErrorBadRequest),
		},
		{
			name: "group upgraded to a supergroup",
			err:  &bot.MigrateError{Message: "Bad Request: group chat was upgraded", MigrateToChatID: -100500},
		},
		{
			name: "rate limited",
			err:  &bot.TooManyRequestsError{Message: "Too Many Requests", RetryAfter: 30},
		},
		{
			name: "network failure",
			err:  errors.New("error do request for method sendMessage, context deadline exceeded"),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isMarkupError(tt.err); got != tt.want {
				t.Errorf("isMarkupError(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}
