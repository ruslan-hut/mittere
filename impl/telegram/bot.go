package telegram

import (
	"fmt"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api"
	"log/slog"
	"mittere/entity"
	"mittere/internal/lib/sl"
	"sync"
)

type Repository interface {
	GetSubscriptions() ([]entity.Subscription, error)
	GetSubscription(id int) (*entity.Subscription, error)
	UpdateSubscription(subscription *entity.Subscription) error
}

// TgBot implements EventHandler
type TgBot struct {
	mu            sync.RWMutex
	api           *tgbotapi.BotAPI
	database      Repository
	subscriptions map[int]entity.Subscription
	invites       []string
	event         chan MessageContent
	send          chan MessageContent
	done          chan struct{}
	log           *slog.Logger
}

type MessageContent struct {
	ChatID int64
	Text   string
}

func New(apiKey string, log *slog.Logger) (*TgBot, error) {
	tgBot := &TgBot{
		subscriptions: make(map[int]entity.Subscription),
		event:         make(chan MessageContent, 100),
		send:          make(chan MessageContent, 100),
		done:          make(chan struct{}),
		log:           log.With(sl.Module("telegram")),
	}
	api, err := tgbotapi.NewBotAPI(apiKey)
	if err != nil {
		return nil, err
	}
	tgBot.log.With(sl.Secret("api_key", apiKey)).Debug("telegram bot created")
	tgBot.api = api
	return tgBot, nil
}

// SetDatabase attach database service
func (b *TgBot) SetDatabase(database Repository) {
	b.database = database
}

func (b *TgBot) Start() {
	b.subscriptions = make(map[int]entity.Subscription)
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
	go b.sendPump()
	go b.eventPump()
	go b.updatesPump()
}

// Stop gracefully shuts down the bot
func (b *TgBot) Stop() {
	b.api.StopReceivingUpdates()
	close(b.done)
	close(b.event)
	close(b.send)
	b.log.Info("telegram bot stopped")
}

// updatesPump listens for Telegram updates
func (b *TgBot) updatesPump() {
	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60
	updates, err := b.api.GetUpdatesChan(u)
	if err != nil {
		b.log.Error("getting updates", sl.Err(err))
		return
	}
	for update := range updates {
		if update.Message == nil {
			continue
		}
		if !update.Message.IsCommand() {
			if b.checkInviteCode(update.Message.Text) {
				b.send <- MessageContent{ChatID: update.Message.Chat.ID, Text: b.confirmSubscription(&update)}
			}
			continue
		}
		switch update.Message.Command() {
		case "start":
			b.send <- MessageContent{ChatID: update.Message.Chat.ID, Text: b.subscribe(&update)}
		case "invite":
			if b.isAdmin(&update) {
				code := generatePinCode()
				b.mu.Lock()
				b.invites = append(b.invites, code)
				b.mu.Unlock()
				b.send <- MessageContent{ChatID: update.Message.Chat.ID, Text: code}
			}
		case "clear":
			if b.isAdmin(&update) {
				b.mu.Lock()
				b.invites = []string{}
				b.mu.Unlock()
				b.send <- MessageContent{ChatID: update.Message.Chat.ID, Text: "Invite codes cleared"}
			}
		case "list":
			if b.isAdmin(&update) {
				b.mu.RLock()
				msg := "Invite codes:\n"
				for _, code := range b.invites {
					msg += fmt.Sprintf("%v\n", code)
				}
				b.mu.RUnlock()
				b.send <- MessageContent{ChatID: update.Message.Chat.ID, Text: msg}
			}
		case "stop":
			b.send <- MessageContent{ChatID: update.Message.Chat.ID, Text: b.deleteSubscription(&update)}
		case "test":
			msg := fmt.Sprintf("*%v*: `%v`\n %v", "MONITOR", "Warn", "This is a test notification, relax")
			b.send <- MessageContent{ChatID: update.Message.Chat.ID, Text: msg}
		default:
			b.send <- MessageContent{ChatID: update.Message.Chat.ID, Text: "Unknown command"}
		}
	}
}

// eventPump sending events to all subscribers
func (b *TgBot) eventPump() {
	for event := range b.event {
		b.mu.RLock()
		active := make([]int64, 0, len(b.subscriptions))
		for _, subscription := range b.subscriptions {
			if subscription.IsActive() {
				active = append(active, int64(subscription.UserID))
			}
		}
		b.mu.RUnlock()
		for _, chatID := range active {
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
	msg := tgbotapi.NewMessage(id, text)
	msg.ParseMode = "MarkdownV2"
	_, err := b.api.Send(msg)
	if err != nil {
		b.log.Warn("sending message", sl.Err(err))
		safeMsg := tgbotapi.NewMessage(id, fmt.Sprintf("This message caused an error:\n%v", removeMarkup(text)))
		_, err = b.api.Send(safeMsg)
		if err != nil {
			b.log.Error("sending no markup message", sl.Err(err))
			// maybe error was while parsing, so we can send a message about this error
			msg = tgbotapi.NewMessage(id, fmt.Sprintf("Error: %v", err))
			_, err = b.api.Send(msg)
			if err != nil {
				b.log.Error("sending message", sl.Err(err))
			}
		}
	}
}

func (b *TgBot) SendEventMessage(em *entity.EventMessage) error {
	msg := fmt.Sprintf("*%v*: `#%v`\n", em.Type, em.Subject)
	if em.Text != "" {
		msg += fmt.Sprintf("%v\n", sanitize(em.Text))
	}
	if em.Payload != nil {
		payload := fmt.Sprintf("%v\n", em.Payload)
		msg += fmt.Sprintf("```\n%v\n```", sanitize(payload))
	}
	//if em.Sender != nil {
	//	msg += fmt.Sprintf("\n`%v`", em.Sender.Name)
	//}
	b.event <- MessageContent{Text: msg}
	return nil
}
