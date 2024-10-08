package telegram

import (
	"fmt"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api"
	"log/slog"
	"mittere/entity"
	"mittere/internal/lib/sl"
	"strings"
)

type Repository interface {
	GetSubscriptions() ([]entity.Subscription, error)
	AddSubscription(subscription *entity.Subscription) error
	DeleteSubscription(subscription *entity.Subscription) error
}

// TgBot implements EventHandler
type TgBot struct {
	api           *tgbotapi.BotAPI
	database      Repository
	subscriptions map[int]entity.Subscription
	event         chan MessageContent
	send          chan MessageContent
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
		log:           log.With(sl.Module("telegram")),
	}
	api, err := tgbotapi.NewBotAPI(apiKey)
	if err != nil {
		return nil, err
	}
	tgBot.log.With(sl.Secret("api_key", apiKey)).Info("telegram bot created")
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
	}
	go b.sendPump()
	go b.eventPump()
	go b.updatesPump()
}

// Start listening for updates
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
			continue
		}
		switch update.Message.Command() {
		case "start":
			subscription := entity.Subscription{
				UserID:           update.Message.From.ID,
				User:             update.Message.From.UserName,
				SubscriptionType: "status",
			}
			b.subscriptions[update.Message.From.ID] = subscription
			msg := fmt.Sprintf("Hello *%v*, you are now subscribed to updates", update.Message.From.UserName)
			if b.database != nil {
				err = b.database.AddSubscription(&subscription)
				if err != nil {
					b.log.Error("adding subscription", sl.Err(err))
					msg = fmt.Sprintf("Error adding subscription:\n `%v`", err)
				}
			}
			b.send <- MessageContent{ChatID: update.Message.Chat.ID, Text: msg}
		case "stop":
			delete(b.subscriptions, update.Message.From.ID)
			if b.database != nil {
				err = b.database.DeleteSubscription(&entity.Subscription{UserID: update.Message.From.ID})
				if err != nil {
					b.log.Error("deleting subscription", sl.Err(err))
				}
			}
			b.send <- MessageContent{ChatID: update.Message.Chat.ID, Text: "Your subscription has been removed"}
		case "test":
			msg := fmt.Sprintf("*%v*: Connector %v: `%v`", "ChargePointId", 1, "Status")
			b.send <- MessageContent{ChatID: update.Message.Chat.ID, Text: msg}
		default:
			b.send <- MessageContent{ChatID: update.Message.Chat.ID, Text: "Unknown command"}
		}
	}
}

// eventPump sending events to all subscribers
func (b *TgBot) eventPump() {
	for {
		if event, ok := <-b.event; ok {
			for _, subscription := range b.subscriptions {
				b.sendMessage(int64(subscription.UserID), event.Text)
			}
		}
	}
}

// sendPump sending messages to users
func (b *TgBot) sendPump() {
	for {
		if event, ok := <-b.send; ok {
			go b.sendMessage(event.ChatID, event.Text)
		}
	}
}

// sendMessage common routine to send a message via bot API
func (b *TgBot) sendMessage(id int64, text string) {
	msg := tgbotapi.NewMessage(id, text)
	msg.ParseMode = "MarkdownV2"
	_, err := b.api.Send(msg)
	if err != nil {
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

func (b *TgBot) OnStatusNotification(event *entity.EventMessage) {
	// only send notifications about Faulted status
	if event.Status != "Faulted" {
		return
	}
	var msg string
	msg = fmt.Sprintf("*%v*: `%v`\n", event.Type, event.Status)
	if event.Info != "" {
		msg += fmt.Sprintf("%v\n", sanitize(event.Info))
	}
	b.event <- MessageContent{Text: msg}
}

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
	// Define a list of reserved characters that need to be escaped
	reservedChars := "\\`*_{}[]()#+-.!|"

	// Loop through each character in the input string
	sanitized := ""
	for _, char := range input {
		// Check if the character is reserved
		if strings.ContainsRune(reservedChars, char) {
			// Escape the character with a backslash
			sanitized += "\\" + string(char)
		} else {
			// Add the character to the sanitized string
			sanitized += string(char)
		}
	}

	return sanitized
}
