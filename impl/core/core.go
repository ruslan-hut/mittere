package core

import (
	"context"
	"crypto/subtle"
	"errors"
	"log/slog"
	"mittere/entity"
	"mittere/internal/lib/sl"
	"time"
)

// Errors the HTTP layer maps onto status codes.
var (
	ErrNoRepository = errors.New("repository not initialized")
	ErrNoTransport  = errors.New("message service not initialized")
	ErrUnknownTopic = errors.New("unknown topic")
	ErrForbidden    = errors.New("topic not allowed for this sender")
	ErrNotFound     = entity.ErrNotFound
	ErrConflict     = entity.ErrConflict
	ErrInProgress   = entity.ErrInProgress
)

// Repository is the data access the service needs.
type Repository interface {
	Ping(ctx context.Context) error

	GetChannels() ([]entity.Channel, error)
	GetChannel(channelID string) (*entity.Channel, error)
	SaveChannel(channel *entity.Channel) error
	DeleteChannel(channelID string) error
	GetChannelsByTopic(topic string) ([]entity.Channel, error)
	GetChannelsByUsername(username string) ([]entity.Channel, error)
	CountChannels(ctx context.Context) (int64, error)

	GetTopics() ([]entity.Topic, error)
	GetTopic(code string) (*entity.Topic, error)
	CreateTopic(topic *entity.Topic) error
	EnsureTopic(topic *entity.Topic) error
	UpdateTopic(topic *entity.Topic) error
	DeleteTopic(code string) error

	Subscribe(sub *entity.Subscription) error
	Unsubscribe(topic, channelID string) error
	GetTopicsOfChannel(channelID string) ([]string, error)
	SetTopicsOfChannel(channelID string, topics []string, by string) error

	GetSenderByToken(token string) (*entity.Sender, error)
	GetSenders() ([]entity.Sender, error)
	GetSender(username string) (*entity.Sender, error)
	CreateSender(sender *entity.Sender) error
	UpdateSender(sender *entity.Sender) error
	DeleteSender(username string) error

	// ClaimEvent takes an event for delivery, returning what an earlier call
	// with the same dedup key left behind, or entity.ErrInProgress while
	// another call is delivering it.
	ClaimEvent(event *entity.Event, lease time.Duration) (*entity.Event, error)
	SaveEvent(event *entity.Event) error
	GetEvent(eventID string) (*entity.Event, error)
	GetEventByDedupKey(key string) (*entity.Event, error)
	FindEvents(filter entity.EventFilter) ([]entity.Event, error)

	CreateLinkCode(code *entity.LinkCode) error
	GetLinkCode(code string) (*entity.LinkCode, error)
	UseLinkCode(code, channelID string, usedAt time.Time) error
}

// Transport delivers a formatted message to one chat. Rate limiting and the
// retry after a 429 belong to the transport; it reports how many attempts it
// made so the journal can record them.
type Transport interface {
	Format(msg *entity.Message) string
	Send(ctx context.Context, chatID int64, threadID int, text string) (messageID int, attempts int, err error)
	BotName() string
	Alive() bool
}

// Core holds the business logic: authentication, routing and dispatch.
type Core struct {
	repo        Repository
	transport   Transport
	token       string
	linkCodeTTL time.Duration
	eventLease  time.Duration
	startedAt   time.Time
	log         *slog.Logger
}

func New(repo Repository, log *slog.Logger) *Core {
	return &Core{
		repo:        repo,
		linkCodeTTL: 30 * time.Minute,
		eventLease:  3 * time.Minute,
		startedAt:   time.Now().UTC(),
		log:         log.With(sl.Module("core")),
	}
}

// SetTransport attaches the message transport.
func (c *Core) SetTransport(t Transport) {
	c.transport = t
}

// SetToken sets the static administrative token from the configuration.
func (c *Core) SetToken(token string) {
	c.token = token
}

// SetLinkCodeTTL sets how long a chat linking code stays usable.
func (c *Core) SetLinkCodeTTL(ttl time.Duration) {
	if ttl > 0 {
		c.linkCodeTTL = ttl
	}
}

// AuthenticateByToken resolves a Bearer token to a sender. The static token
// from the configuration is an administrator.
func (c *Core) AuthenticateByToken(token string) (*entity.Sender, error) {
	if token == "" {
		return nil, errors.New("token not provided")
	}
	if c.token != "" && subtle.ConstantTimeCompare([]byte(c.token), []byte(token)) == 1 {
		return &entity.Sender{
			Username: "system",
			Name:     "system",
			Scope:    entity.ScopeAdmin,
			State:    entity.StateActive,
		}, nil
	}
	if c.repo == nil {
		return nil, ErrNoRepository
	}
	sender, err := c.repo.GetSenderByToken(token)
	if err != nil {
		c.log.With(sl.Secret("token", token)).Error("read sender", sl.Err(err))
		return nil, errors.New("sender not found")
	}
	if sender == nil {
		return nil, errors.New("sender not found")
	}
	if !sender.IsActive() {
		return nil, errors.New("sender is not active")
	}
	return sender, nil
}
