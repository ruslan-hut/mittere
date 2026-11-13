package core

import (
	"context"
	"fmt"
	"mittere/entity"
	"time"
)

// --- topics ---

func (c *Core) GetTopics() ([]entity.Topic, error) {
	if c.repo == nil {
		return nil, ErrNoRepository
	}
	return c.repo.GetTopics()
}

func (c *Core) GetTopic(code string) (*entity.Topic, error) {
	if c.repo == nil {
		return nil, ErrNoRepository
	}
	return c.repo.GetTopic(code)
}

func (c *Core) CreateTopic(topic *entity.Topic) error {
	if c.repo == nil {
		return ErrNoRepository
	}
	if err := entity.ValidateTopicCode(topic.Code); err != nil {
		return err
	}
	if topic.CreatedAt.IsZero() {
		topic.CreatedAt = time.Now().UTC()
	}
	existing, err := c.repo.GetTopic(topic.Code)
	if err != nil {
		return err
	}
	if existing != nil {
		return fmt.Errorf("topic %s: %w", topic.Code, ErrConflict)
	}
	return c.repo.CreateTopic(topic)
}

func (c *Core) UpdateTopic(topic *entity.Topic) error {
	if c.repo == nil {
		return ErrNoRepository
	}
	stored, err := c.repo.GetTopic(topic.Code)
	if err != nil {
		return err
	}
	if stored == nil {
		return fmt.Errorf("topic %s: %w", topic.Code, ErrNotFound)
	}
	// a field the request left out keeps its stored value, and the creation
	// time is never rewritten by an update
	if topic.Title == "" {
		topic.Title = stored.Title
	}
	if topic.Description == "" {
		topic.Description = stored.Description
	}
	if topic.Bot == "" {
		topic.Bot = stored.Bot
	}
	topic.CreatedAt = stored.CreatedAt
	return c.repo.UpdateTopic(topic)
}

func (c *Core) DeleteTopic(code string) error {
	if c.repo == nil {
		return ErrNoRepository
	}
	if code == entity.TopicDefault || code == entity.TopicAdmin {
		return fmt.Errorf("topic %s is built in and cannot be deleted", code)
	}
	return c.repo.DeleteTopic(code)
}

// --- channels ---

// GetChannels returns every channel with the topics it is subscribed to.
func (c *Core) GetChannels() ([]entity.Channel, error) {
	if c.repo == nil {
		return nil, ErrNoRepository
	}
	channels, err := c.repo.GetChannels()
	if err != nil {
		return nil, err
	}
	for i := range channels {
		topics, err := c.repo.GetTopicsOfChannel(channels[i].ChannelID)
		if err != nil {
			return nil, err
		}
		channels[i].Topics = topics
	}
	return channels, nil
}

func (c *Core) GetChannel(channelID string) (*entity.Channel, error) {
	if c.repo == nil {
		return nil, ErrNoRepository
	}
	channel, err := c.repo.GetChannel(channelID)
	if err != nil || channel == nil {
		return channel, err
	}
	topics, err := c.repo.GetTopicsOfChannel(channelID)
	if err != nil {
		return nil, err
	}
	channel.Topics = topics
	return channel, nil
}

// UpdateChannel changes the state, title and subscriptions of a channel. The
// chat identifier itself is set when the chat is linked and never by hand.
func (c *Core) UpdateChannel(channel *entity.Channel, by string) error {
	if c.repo == nil {
		return ErrNoRepository
	}
	stored, err := c.repo.GetChannel(channel.ChannelID)
	if err != nil {
		return err
	}
	if stored == nil {
		return ErrNotFound
	}
	// everything is checked before anything is written, so a bad request does
	// not leave the channel half changed
	if channel.State != "" && !validState(channel.State) {
		return fmt.Errorf("unknown state %s", channel.State)
	}
	for _, code := range channel.Topics {
		topic, err := c.repo.GetTopic(code)
		if err != nil {
			return err
		}
		if topic == nil {
			return fmt.Errorf("%w: %s", ErrUnknownTopic, code)
		}
	}

	if channel.State != "" {
		stored.State = channel.State
	}
	if channel.Title != "" {
		stored.Title = channel.Title
	}
	if err = c.repo.SaveChannel(stored); err != nil {
		return err
	}
	if channel.Topics != nil {
		return c.repo.SetTopicsOfChannel(channel.ChannelID, channel.Topics, by)
	}
	return nil
}

func validState(state string) bool {
	switch state {
	case entity.StateActive, entity.StateAwait, entity.StateDisabled:
		return true
	}
	return false
}

func (c *Core) DeleteChannel(channelID string) error {
	if c.repo == nil {
		return ErrNoRepository
	}
	return c.repo.DeleteChannel(channelID)
}

// CreateLinkCode issues a code to be sent in a chat with the bot's link
// command. The chat identifier is never typed by hand: the bot reads it from
// the message carrying the code.
func (c *Core) CreateLinkCode(topic, by string) (*entity.LinkCode, error) {
	if c.repo == nil {
		return nil, ErrNoRepository
	}
	if topic != "" {
		known, err := c.repo.GetTopic(topic)
		if err != nil {
			return nil, err
		}
		if known == nil {
			return nil, fmt.Errorf("%w: %s", ErrUnknownTopic, topic)
		}
	}
	now := time.Now().UTC()
	code := &entity.LinkCode{
		Code:      newCode(),
		Topic:     topic,
		CreatedAt: now,
		ExpiresAt: now.Add(c.linkCodeTTL),
		CreatedBy: by,
	}
	if err := c.repo.CreateLinkCode(code); err != nil {
		return nil, err
	}
	return code, nil
}

// --- senders ---

func (c *Core) GetSenders() ([]entity.Sender, error) {
	if c.repo == nil {
		return nil, ErrNoRepository
	}
	return c.repo.GetSenders()
}

func (c *Core) GetSender(username string) (*entity.Sender, error) {
	if c.repo == nil {
		return nil, ErrNoRepository
	}
	return c.repo.GetSender(username)
}

func (c *Core) CreateSender(sender *entity.Sender) error {
	if c.repo == nil {
		return ErrNoRepository
	}
	if sender.Token == "" {
		sender.Token = newToken()
	}
	return c.repo.CreateSender(sender)
}

func (c *Core) UpdateSender(sender *entity.Sender) error {
	if c.repo == nil {
		return ErrNoRepository
	}
	stored, err := c.repo.GetSender(sender.Username)
	if err != nil {
		return err
	}
	if stored == nil {
		return ErrNotFound
	}
	// a field the request left out keeps its stored value: an update without
	// allowed_topics must not silently widen a sender to every topic, and one
	// without a state must not re-enable a disabled sender
	if sender.Token == "" {
		sender.Token = stored.Token
	}
	if sender.CreatedAt.IsZero() {
		sender.CreatedAt = stored.CreatedAt
	}
	if sender.AllowedTopics == nil {
		sender.AllowedTopics = stored.AllowedTopics
	}
	if sender.State == "" {
		sender.State = stored.State
	}
	if sender.Scope == "" {
		sender.Scope = stored.Scope
	}
	if sender.Name == "" {
		sender.Name = stored.Name
	}
	return c.repo.UpdateSender(sender)
}

func (c *Core) DeleteSender(username string) error {
	if c.repo == nil {
		return ErrNoRepository
	}
	return c.repo.DeleteSender(username)
}

// --- health ---

// Health describes the state of the service for monitoring.
type Health struct {
	Status      string     `json:"status"`
	Uptime      string     `json:"uptime"`
	Bot         string     `json:"bot,omitempty"`
	BotAlive    bool       `json:"bot_alive"`
	Database    string     `json:"database"`
	Channels    int64      `json:"channels,omitempty"`
	Topics      int        `json:"topics,omitempty"`
	LastFailure *time.Time `json:"last_failure,omitempty"`
	Failures24h int        `json:"failures_24h"`
	Errors      []string   `json:"errors,omitempty"`
}

// GetHealth collects the state of the bot, the database and the recent failures.
func (c *Core) GetHealth(ctx context.Context, detailed bool) *Health {
	health := &Health{
		Status:   "ok",
		Uptime:   time.Since(c.startedAt).Truncate(time.Second).String(),
		Database: "disabled",
	}
	if c.transport != nil {
		health.Bot = c.transport.BotName()
		health.BotAlive = c.transport.Alive()
		if !health.BotAlive {
			health.Status = "degraded"
		}
	} else {
		health.Status = "degraded"
	}
	if c.repo != nil {
		health.Database = "ok"
		if err := c.repo.Ping(ctx); err != nil {
			health.Database = "error"
			health.Status = "degraded"
		} else if detailed {
			if count, err := c.repo.CountChannels(ctx); err == nil {
				health.Channels = count
			}
			if topics, err := c.repo.GetTopics(); err == nil {
				health.Topics = len(topics)
			}
			c.recentFailures(health)
		}
	}
	return health
}

func (c *Core) recentFailures(health *Health) {
	from := time.Now().UTC().Add(-24 * time.Hour)
	events, err := c.repo.FindEvents(entity.EventFilter{From: from, Limit: 200})
	if err != nil {
		return
	}
	for i := range events {
		if events[i].Status != entity.EventFailed && events[i].Status != entity.EventPartial {
			continue
		}
		health.Failures24h++
		if health.LastFailure == nil || events[i].CreatedAt.After(*health.LastFailure) {
			createdAt := events[i].CreatedAt
			health.LastFailure = &createdAt
		}
		if len(health.Errors) < 5 {
			for _, delivery := range events[i].Deliveries {
				if delivery.Status == entity.DeliveryFailed && delivery.Error != "" {
					health.Errors = append(health.Errors, fmt.Sprintf("%s %s: %s", events[i].Topic, delivery.ChannelID, delivery.Error))
					break
				}
			}
		}
	}
}
