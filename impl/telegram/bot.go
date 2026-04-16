package telegram

import (
	"context"
	"fmt"
	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"log/slog"
	"mittere/entity"
	"mittere/internal/lib/sl"
	"sync"
)

type Repository interface {
	GetSubscriptions() ([]entity.Subscription, error)
	GetSubscription(id int64) (*entity.Subscription, error)
	UpdateSubscription(subscription *entity.Subscription) error
}

// TgBot implements EventHandler
type TgBot struct {
	mu            sync.RWMutex
	bot           *bot.Bot
	cancel        context.CancelFunc
	database      Repository
	subscriptions map[int64]entity.Subscription
	invites       []string
	event         chan MessageContent
	send          chan MessageContent
	log           *slog.Logger
}

type MessageContent struct {
	ChatID   int64
	Text     string
	Role     string // target role: "admin" = admins only, "user" or "" = all active
	Username string // if set, deliver only to this username
}

func New(apiKey string, log *slog.Logger) (*TgBot, error) {
	tgBot := &TgBot{
		subscriptions: make(map[int64]entity.Subscription),
		event:         make(chan MessageContent, 100),
		send:          make(chan MessageContent, 100),
		log:           log.With(sl.Module("telegram")),
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

// SetDatabase attach database service
func (b *TgBot) SetDatabase(database Repository) {
	b.database = database
}

func (b *TgBot) Start() {
	b.subscriptions = make(map[int64]entity.Subscription)
	if b.database != nil {
		subscriptions, err := b.database.GetSubscriptions()
		if err != nil {
			b.log.Error("getting subscriptions", sl.Err(err))
		}
		if subscriptions != nil {
			for _, subscription := range subscriptions {
				b.subscriptions[subscription.UserID] = subscription
			}
		}
		b.log.With(slog.Int("count", len(b.subscriptions))).Info("subscriptions loaded")
	}

	ctx, cancel := context.WithCancel(context.Background())
	b.cancel = cancel

	go b.sendPump()
	go b.eventPump()
	go b.bot.Start(ctx)
}

// Stop gracefully shuts down the bot
func (b *TgBot) Stop() {
	if b.cancel != nil {
		b.cancel()
	}
	close(b.event)
	close(b.send)
	b.log.Info("telegram bot stopped")
}

// handleUpdate processes incoming Telegram updates
func (b *TgBot) handleUpdate(_ context.Context, _ *bot.Bot, update *models.Update) {
	if update.Message == nil {
		return
	}

	cmd := extractCommand(update.Message.Text)
	if cmd == "" {
		if b.checkInviteCode(update.Message.Text) {
			b.send <- MessageContent{ChatID: update.Message.Chat.ID, Text: b.confirmSubscription(update)}
		}
		return
	}

	switch cmd {
	case "start":
		b.send <- MessageContent{ChatID: update.Message.Chat.ID, Text: b.subscribe(update)}
	case "invite":
		if b.isAdmin(update) {
			code := generatePinCode()
			b.mu.Lock()
			b.invites = append(b.invites, code)
			b.mu.Unlock()
			b.send <- MessageContent{ChatID: update.Message.Chat.ID, Text: code}
		}
	case "clear":
		if b.isAdmin(update) {
			b.mu.Lock()
			b.invites = []string{}
			b.mu.Unlock()
			b.send <- MessageContent{ChatID: update.Message.Chat.ID, Text: "Invite codes cleared"}
		}
	case "list":
		if b.isAdmin(update) {
			b.mu.RLock()
			msg := "Invite codes:\n"
			for _, code := range b.invites {
				msg += fmt.Sprintf("%v\n", code)
			}
			b.mu.RUnlock()
			b.send <- MessageContent{ChatID: update.Message.Chat.ID, Text: msg}
		}
	case "stop":
		b.send <- MessageContent{ChatID: update.Message.Chat.ID, Text: b.deleteSubscription(update)}
	case "test":
		msg := fmt.Sprintf("*%v*: `%v`\n %v", "MONITOR", "Warn", "This is a test notification, relax")
		b.send <- MessageContent{ChatID: update.Message.Chat.ID, Text: msg}
	default:
		b.send <- MessageContent{ChatID: update.Message.Chat.ID, Text: "Unknown command"}
	}
}

// eventPump sending events to subscribers filtered by role and username
func (b *TgBot) eventPump() {
	for event := range b.event {
		b.mu.RLock()
		recipients := make([]int64, 0, len(b.subscriptions))
		for _, sub := range b.subscriptions {
			if !sub.IsActive() {
				continue
			}
			// username targeting: deliver only to the specified user
			if event.Username != "" {
				if sub.User == event.Username {
					recipients = append(recipients, sub.UserID)
				}
				continue
			}
			// role filtering: "admin" messages go to admins only, others go to everyone
			if event.Role == entity.RoleAdmin && !sub.IsAdmin() {
				continue
			}
			recipients = append(recipients, sub.UserID)
		}
		b.mu.RUnlock()
		for _, chatID := range recipients {
			b.sendMessage(chatID, event.Text)
		}
	}
}

// sendPump sending messages to users
func (b *TgBot) sendPump() {
	for event := range b.send {
		go b.sendMessage(event.ChatID, event.Text)
	}
}

// sendMessage common routine to send a message via bot API
func (b *TgBot) sendMessage(id int64, text string) {
	_, err := b.bot.SendMessage(context.Background(), &bot.SendMessageParams{
		ChatID:    id,
		Text:      text,
		ParseMode: models.ParseModeMarkdown,
	})
	if err != nil {
		b.log.Warn("sending message", sl.Err(err))
		_, err = b.bot.SendMessage(context.Background(), &bot.SendMessageParams{
			ChatID: id,
			Text:   fmt.Sprintf("This message caused an error:\n%v", removeMarkup(text)),
		})
		if err != nil {
			b.log.Error("sending no markup message", sl.Err(err))
			_, err = b.bot.SendMessage(context.Background(), &bot.SendMessageParams{
				ChatID: id,
				Text:   fmt.Sprintf("Error: %v", err),
			})
			if err != nil {
				b.log.Error("sending message", sl.Err(err))
			}
		}
	}
}

func (b *TgBot) SendEventMessage(em *entity.EventMessage) error {
	msg := fmt.Sprintf("*%v*: `#%v`\n", em.Type, em.Subject)
	if em.Text != "" {
		msg += fmt.Sprintf("%v\n", bot.EscapeMarkdown(em.Text))
	}
	if em.Payload != nil {
		payload := fmt.Sprintf("%v\n", em.Payload)
		msg += fmt.Sprintf("```\n%v\n```", bot.EscapeMarkdown(payload))
	}

	role := em.Role
	if role == "" {
		role = entity.RoleUser
	}

	b.event <- MessageContent{
		Text:     msg,
		Role:     role,
		Username: em.Username,
	}
	return nil
}
