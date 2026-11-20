package telegram

import (
	"testing"

	"mittere/entity"
)

func TestExtractCommand(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		wantCmd  string
		wantArgs string
	}{
		{"plain command", "/start", "start", ""},
		{"command with argument", "/link ABCD-1234", "link", "ABCD-1234"},
		{"command addressed to the bot in a group", "/link@mittere_bot ABCD-1234", "link", "ABCD-1234"},
		{"mention without argument", "/status@mittere_bot", "status", ""},
		{"uppercase command", "/Start", "start", ""},
		{"leading space", "  /stop", "stop", ""},
		{"plain text is not a command", "ABCD-1234", "", ""},
		{"empty text", "", "", ""},
		{"argument on the next line", "/link\nABCD-1234", "link", "ABCD-1234"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd, args := extractCommand(tt.text)
			if cmd != tt.wantCmd || args != tt.wantArgs {
				t.Errorf("extractCommand(%q) = (%q, %q), want (%q, %q)", tt.text, cmd, args, tt.wantCmd, tt.wantArgs)
			}
		})
	}
}

func TestLooksLikeCode(t *testing.T) {
	tests := []struct {
		text string
		want bool
	}{
		{"ABCD-1234", true},
		{"abcd-1234", true},
		{" ABCD-1234 ", true},
		{"ABCD1234", false},
		{"ABCD-12345", false},
		{"hello", false},
		{"", false},
	}
	for _, tt := range tests {
		t.Run(tt.text, func(t *testing.T) {
			if got := looksLikeCode(tt.text); got != tt.want {
				t.Errorf("looksLikeCode(%q) = %v, want %v", tt.text, got, tt.want)
			}
		})
	}
}

func TestFormat(t *testing.T) {
	tests := []struct {
		name string
		msg  entity.Message
		want string
	}{
		{
			name: "ordinary message keeps type and tag",
			msg:  entity.Message{Topic: "rior.eventlog", Type: "error", Subject: "Ошибка проведения", Text: "Док. 12."},
			want: "*error*: `#Ошибка_проведения`\nДок\\. 12\\.\n",
		},
		{
			name: "tag keeps letters and digits joined by underscores",
			msg:  entity.Message{Topic: "rior.eventlog", Type: "Журнал подій", Subject: "Обмен с сайтом: B2B Україна (v2.1)"},
			want: "*Журнал подій*: `#Обмен_с_сайтом_B2B_Україна_v2_1`\n",
		},
		{
			name: "spaces in the tag become underscores",
			msg:  entity.Message{Topic: "marketing.shipments", Type: "Маркетингові відправки", Subject: "Маркетингові відправки"},
			want: "*Маркетингові відправки*: `#Маркетингові_відправки`\n",
		},
		{
			name: "subject falls back to topic",
			msg:  entity.Message{Topic: "rior.eventlog", Type: "info"},
			want: "*info*: `#rior_eventlog`\n",
		},
		{
			name: "ordinary resolved type is not an outcome",
			msg:  entity.Message{Topic: "t", Type: "resolved", Subject: "s"},
			want: "*resolved*: `#s`\n",
		},
		{
			name: "outcome shows the bold escaped subject only",
			msg:  entity.Message{Topic: "rior.eventlog", Type: "resolved", Subject: "✅ Решено · Ошибка в *РН-12* (v2.1)", Text: "Исправлено.", Resolves: "Tb34dc0d1"},
			want: "*✅ Решено · Ошибка в \\*РН\\-12\\* \\(v2\\.1\\)*\nИсправлено\\.\n",
		},
	}
	b := &TgBot{}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := b.Format(&tt.msg); got != tt.want {
				t.Errorf("Format() = %q, want %q", got, tt.want)
			}
		})
	}
}
