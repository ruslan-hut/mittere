package core

import (
	"fmt"
	"log/slog"
	"mittere/entity"
	"mittere/internal/lib/sl"
	"strings"
	"time"
)

// StartChat handles the start command in a private chat: the chat is registered
// as a channel and waits for a linking code before it receives anything.
func (c *Core) StartChat(chat *entity.ChatInfo, code string) string {
	if c.repo == nil {
		return "Service is not ready"
	}
	if code != "" {
		return c.LinkChat(chat, code)
	}
	channel, err := c.repo.GetChannel(chat.ChannelID())
	if err != nil {
		c.log.Error("read channel", sl.Err(err))
		return "Service is not ready"
	}
	if channel != nil && channel.IsActive() {
		topics, _ := c.repo.GetTopicsOfChannel(channel.ChannelID)
		return fmt.Sprintf("Already linked\nChannel: %s\nTopics: %s", channel.ChannelID, topicList(topics))
	}
	if channel == nil {
		fresh := entity.NewChannel(chat.ChatID, chat.ThreadID, entity.ChannelPrivate)
		fresh.State = entity.StateAwait
		fresh.Title = chat.Title
		fresh.Username = chat.Username
		fresh.LinkedBy = chat.ActorUsername
		if err = c.repo.SaveChannel(&fresh); err != nil {
			c.log.Error("save channel", sl.Err(err))
			return "Failed to register this chat"
		}
	}
	return "Registered. Send the linking code to start receiving notifications"
}

// LinkChat spends a linking code on the chat it was sent from. The chat
// identifier comes from the message, so nothing has to be typed by hand and the
// bot's privacy mode in groups is not in the way.
func (c *Core) LinkChat(chat *entity.ChatInfo, code string) string {
	if c.repo == nil {
		return "Service is not ready"
	}
	code = strings.ToUpper(strings.TrimSpace(code))
	if code == "" {
		return "Send the command with a linking code: link CODE"
	}
	stored, err := c.repo.GetLinkCode(code)
	if err != nil {
		c.log.Error("read link code", sl.Err(err))
		return "Service is not ready"
	}
	now := time.Now().UTC()
	if stored == nil || !stored.IsUsable(now) {
		return "This code is unknown, expired or already used"
	}

	channelID := chat.ChannelID()
	channel, err := c.repo.GetChannel(channelID)
	if err != nil {
		c.log.Error("read channel", sl.Err(err))
		return "Service is not ready"
	}
	if channel == nil {
		fresh := entity.NewChannel(chat.ChatID, chat.ThreadID, chat.Kind)
		channel = &fresh
	}
	channel.Kind = chat.Kind
	channel.Title = chat.Title
	channel.Username = chat.Username
	channel.State = entity.StateActive
	channel.LinkedAt = now
	channel.LinkedBy = chat.ActorUsername
	channel.LinkedByID = chat.ActorID

	if err = c.repo.UseLinkCode(code, channelID, now); err != nil {
		return "This code is unknown, expired or already used"
	}
	if err = c.repo.SaveChannel(channel); err != nil {
		c.log.Error("save channel", sl.Err(err))
		return "Failed to link this chat"
	}
	if stored.Topic != "" {
		sub := entity.NewSubscription(stored.Topic, channelID, chat.ActorUsername)
		if err = c.repo.Subscribe(&sub); err != nil {
			c.log.Error("subscribe channel", sl.Err(err))
			return "Chat is linked, but the topic was not attached"
		}
	}
	c.log.With(
		slog.String("channel", channelID),
		slog.String("topic", stored.Topic),
	).Info("chat linked")

	topics, _ := c.repo.GetTopicsOfChannel(channelID)
	return fmt.Sprintf("Linked\nChannel: %s\nTopics: %s", channelID, topicList(topics))
}

// StopChat disables a channel; its subscriptions are kept, so linking it again
// resumes where it stopped.
func (c *Core) StopChat(chat *entity.ChatInfo) string {
	if c.repo == nil {
		return "Service is not ready"
	}
	channel, err := c.repo.GetChannel(chat.ChannelID())
	if err != nil {
		c.log.Error("read channel", sl.Err(err))
		return "Service is not ready"
	}
	if channel == nil {
		return "This chat is not linked"
	}
	// in a group anyone could silently switch off the notifications the whole
	// chat relies on, so only the person who linked it may stop it; in a
	// private chat it is the person's own channel
	if !chat.IsPrivate() && channel.LinkedByID != 0 && channel.LinkedByID != chat.ActorID {
		return fmt.Sprintf("Only %s, who linked this chat, can stop it; an administrator can disable it through the API", linkedByName(channel))
	}
	channel.State = entity.StateDisabled
	if err = c.repo.SaveChannel(channel); err != nil {
		c.log.Error("save channel", sl.Err(err))
		return "Failed to stop notifications"
	}
	return "Notifications stopped for this chat"
}

// StatusChat reports what the service knows about a chat.
func (c *Core) StatusChat(chat *entity.ChatInfo) string {
	if c.repo == nil {
		return "Service is not ready"
	}
	channel, err := c.repo.GetChannel(chat.ChannelID())
	if err != nil {
		c.log.Error("read channel", sl.Err(err))
		return "Service is not ready"
	}
	if channel == nil {
		return "This chat is not linked"
	}
	topics, _ := c.repo.GetTopicsOfChannel(channel.ChannelID)
	return fmt.Sprintf("Channel: %s\nKind: %s\nState: %s\nTopics: %s",
		channel.ChannelID, channel.Kind, channel.State, topicList(topics))
}

func linkedByName(channel *entity.Channel) string {
	if channel.LinkedBy != "" {
		return "@" + channel.LinkedBy
	}
	return "the person who linked this chat"
}

func topicList(topics []string) string {
	if len(topics) == 0 {
		return "none"
	}
	return strings.Join(topics, ", ")
}
