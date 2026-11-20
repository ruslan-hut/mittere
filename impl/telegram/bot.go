package telegram

import (
	"context"
	"errors"
	"fmt"
	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"log/slog"
	"mittere/entity"
	"mittere/internal/lib/sl"
	"strings"
	"sync"
	"time"
)

// maxRetryAfter caps how long a 429 answer may hold a delivery.
const maxRetryAfter = 60 * time.Second

// Commands is the business logic behind the bot commands, implemented by core.
type Commands interface {
	StartChat(chat *entity.ChatInfo, code string) string
	LinkChat(chat *entity.ChatInfo, code string) string
	StopChat(chat *entity.ChatInfo) string
	StatusChat(chat *entity.ChatInfo) string
}

// TgBot is the Telegram transport: it sends messages and serves the chat
// commands that link chats to topics.
type TgBot struct {
	mu       sync.RWMutex
	bot      *bot.Bot
	cancel   context.CancelFunc
	commands Commands
	limit    *limiter
	botName  string
	alive    bool
	log      *slog.Logger
}

// New creates the bot. Rate is the number of messages allowed to one chat per
// window; Telegram allows about 20 a minute to a group.
func New(apiKey string, rate int, window time.Duration, log *slog.Logger) (*TgBot, error) {
	tgBot := &TgBot{
		limit: newLimiter(rate, window),
		log:   log.With(sl.Module("telegram")),
	}

	opts := []bot.Option{
		bot.WithDefaultHandler(tgBot.handleUpdate),
	}

	b, err := bot.New(apiKey, opts...)
	if err != nil {
		return nil, err
	}
	tgBot.bot = b
	return tgBot, nil
}

// SetCommands attaches the handler of the bot commands.
func (b *TgBot) SetCommands(commands Commands) {
	b.commands = commands
}

// Start begins long polling for updates.
func (b *TgBot) Start() {
	ctx, cancel := context.WithCancel(context.Background())
	b.cancel = cancel

	meCtx, meCancel := context.WithTimeout(ctx, 10*time.Second)
	me, err := b.bot.GetMe(meCtx)
	meCancel()
	b.mu.Lock()
	if err != nil {
		b.log.Error("telegram getMe", sl.Err(err))
		b.alive = false
	} else {
		b.botName = me.Username
		b.alive = true
	}
	b.mu.Unlock()

	go b.bot.Start(ctx)
}

// Stop shuts the bot down.
func (b *TgBot) Stop() {
	if b.cancel != nil {
		b.cancel()
	}
	b.mu.Lock()
	b.alive = false
	b.mu.Unlock()
	b.log.Info("telegram bot stopped")
}

// BotName returns the username of the bot.
func (b *TgBot) BotName() string {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.botName
}

// Alive reports whether the Telegram API answered when the bot started.
func (b *TgBot) Alive() bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.alive
}

// Format renders a message as Telegram markdown.
func (b *TgBot) Format(msg *entity.Message) string {
	subject := msg.Subject
	if subject == "" {
		subject = msg.Topic
	}
	var text string
	if msg.Resolves != "" {
		// a task outcome is read by people: the subject alone, as a bold title
		text = fmt.Sprintf("*%v*\n", strings.ReplaceAll(escapeText(subject), "*", `\*`))
	} else {
		text = fmt.Sprintf("*%v*: `#%v`\n", msg.Type, hashtag(subject))
	}
	if msg.Text != "" {
		text += fmt.Sprintf("%v\n", escapeText(msg.Text))
	}
	if msg.Payload != nil {
		payload := fmt.Sprintf("%v\n", msg.Payload)
		text += fmt.Sprintf("```\n%v\n```", escapeCode(payload))
	}
	return text
}

// Send delivers a message to one chat and reports how many attempts it took.
// A 429 answer is honoured: the chat is paused for the retry_after Telegram
// asked for and the message is sent once more. A message the Telegram markdown
// parser rejects is sent once more without markup.
func (b *TgBot) Send(ctx context.Context, chatID int64, threadID int, text string) (int, int, error) {
	attempts := 0
	if err := b.limit.wait(ctx, chatID); err != nil {
		return 0, attempts, err
	}

	attempts++
	messageID, err := b.send(ctx, chatID, threadID, text, true)
	if err == nil {
		return messageID, attempts, nil
	}

	var tooMany *bot.TooManyRequestsError
	if errors.As(err, &tooMany) {
		wait := time.Duration(tooMany.RetryAfter) * time.Second
		if wait <= 0 {
			wait = time.Second
		}
		if wait > maxRetryAfter {
			return 0, attempts, fmt.Errorf("rate limited, retry_after %ds", tooMany.RetryAfter)
		}
		b.limit.pause(chatID, wait)
		b.log.With(slog.Int64("chat_id", chatID), slog.Int("retry_after", tooMany.RetryAfter)).Warn("rate limited by telegram")
		// waiting through the limiter honours the pause and counts the repeat
		// against the window of this chat
		if waitErr := b.limit.wait(ctx, chatID); waitErr != nil {
			return 0, attempts, waitErr
		}
		attempts++
		messageID, err = b.send(ctx, chatID, threadID, text, true)
		if err == nil {
			return messageID, attempts, nil
		}
		return 0, attempts, err
	}

	// only markup the parser rejects is worth sending again without it. Being
	// blocked, kicked or an unknown chat will not go away on a repeat, and
	// repeating a message Telegram may already have accepted would double it
	if !isMarkupError(err) {
		return 0, attempts, err
	}
	b.log.With(slog.Int64("chat_id", chatID)).Warn("markup rejected, sending as plain text", sl.Err(err))
	attempts++
	messageID, plainErr := b.send(ctx, chatID, threadID, removeMarkup(text), false)
	if plainErr != nil {
		return 0, attempts, plainErr
	}
	return messageID, attempts, nil
}

// isMarkupError reports whether Telegram refused the message because of its
// markup, which is the one failure a plain text repeat can fix.
func isMarkupError(err error) bool {
	if !errors.Is(err, bot.ErrorBadRequest) {
		return false
	}
	text := strings.ToLower(err.Error())
	return strings.Contains(text, "parse entities") ||
		strings.Contains(text, "parse markdown") ||
		strings.Contains(text, "entities")
}

func (b *TgBot) send(ctx context.Context, chatID int64, threadID int, text string, markup bool) (int, error) {
	params := &bot.SendMessageParams{
		ChatID:          chatID,
		MessageThreadID: threadID,
		Text:            text,
	}
	if markup {
		params.ParseMode = models.ParseModeMarkdown
	}
	message, err := b.bot.SendMessage(ctx, params)
	if err != nil {
		return 0, err
	}
	return message.ID, nil
}

// Delete removes a message the bot sent. A message that is already gone counts
// as deleted. Telegram refuses messages older than 48 hours and chats where the
// bot has no right to delete messages; that refusal is returned as the error.
func (b *TgBot) Delete(ctx context.Context, chatID int64, messageID int) error {
	if err := b.limit.wait(ctx, chatID); err != nil {
		return err
	}
	_, err := b.bot.DeleteMessage(ctx, &bot.DeleteMessageParams{ChatID: chatID, MessageID: messageID})
	if err != nil && strings.Contains(strings.ToLower(err.Error()), "message to delete not found") {
		return nil
	}
	return err
}

// handleUpdate serves the chat commands. In a group the bot sees only the
// commands addressed to it, which is why a chat is linked by a code sent in the
// chat rather than by typing its identifier somewhere else.
func (b *TgBot) handleUpdate(ctx context.Context, _ *bot.Bot, update *models.Update) {
	if update.Message == nil || b.commands == nil {
		return
	}
	chat := chatInfo(update.Message)
	cmd, args := extractCommand(update.Message.Text)

	var reply string
	switch cmd {
	case "start":
		reply = b.commands.StartChat(chat, args)
	case "link":
		reply = b.commands.LinkChat(chat, args)
	case "stop":
		reply = b.commands.StopChat(chat)
	case "status":
		reply = b.commands.StatusChat(chat)
	case "test":
		reply = "This is a test notification, relax"
	case "":
		// a bare code in a private chat links that chat
		if chat.IsPrivate() && looksLikeCode(update.Message.Text) {
			reply = b.commands.LinkChat(chat, update.Message.Text)
		}
	default:
		if chat.IsPrivate() {
			reply = "Unknown command. Use start, link CODE, status or stop"
		}
	}
	if reply == "" {
		return
	}
	if _, err := b.send(ctx, chat.ChatID, chat.ThreadID, reply, false); err != nil {
		b.log.Error("sending reply", sl.Err(err))
	}
}

func chatInfo(message *models.Message) *entity.ChatInfo {
	chat := &entity.ChatInfo{
		ChatID:   message.Chat.ID,
		ThreadID: message.MessageThreadID,
		Title:    message.Chat.Title,
		Username: message.Chat.Username,
	}
	if chat.Title == "" {
		chat.Title = strings.TrimSpace(message.Chat.FirstName + " " + message.Chat.LastName)
	}
	chat.Kind = entity.ChannelKind(chat.ChatID, chat.ThreadID)
	if message.From != nil {
		chat.ActorID = message.From.ID
		chat.ActorUsername = message.From.Username
	}
	return chat
}
