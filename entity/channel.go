package entity

import (
	"fmt"
	"mittere/internal/lib/validate"
	"net/http"
	"time"
)

// Channel kinds. A private chat of a person is a channel like any other.
const (
	ChannelPrivate = "private"
	ChannelGroup   = "group"
	ChannelForum   = "forum"
)

// Channel is a Telegram chat the service may deliver to. A forum topic inside a
// group is a separate channel, told apart by ThreadID.
type Channel struct {
	ChannelID  string    `json:"channel_id" bson:"channel_id"`
	ChatID     int64     `json:"chat_id" bson:"chat_id" validate:"required"`
	ThreadID   int       `json:"thread_id,omitempty" bson:"thread_id"`
	Kind       string    `json:"kind" bson:"kind"`
	Title      string    `json:"title,omitempty" bson:"title"`
	Username   string    `json:"username,omitempty" bson:"username"`
	State      string    `json:"state" bson:"state"`
	LinkedAt   time.Time `json:"linked_at,omitempty" bson:"linked_at"`
	LinkedBy   string    `json:"linked_by,omitempty" bson:"linked_by"`
	LinkedByID int64     `json:"linked_by_id,omitempty" bson:"linked_by_id"`
	Topics     []string  `json:"topics,omitempty" bson:"-"`
}

// ChannelKey builds the stable identifier of a channel from its chat and thread.
func ChannelKey(chatID int64, threadID int) string {
	if threadID == 0 {
		return fmt.Sprintf("%d", chatID)
	}
	return fmt.Sprintf("%d:%d", chatID, threadID)
}

// NewChannel creates an active channel with its identifier filled in.
func NewChannel(chatID int64, threadID int, kind string) Channel {
	return Channel{
		ChannelID: ChannelKey(chatID, threadID),
		ChatID:    chatID,
		ThreadID:  threadID,
		Kind:      kind,
		State:     StateActive,
		LinkedAt:  time.Now().UTC(),
	}
}

func (c *Channel) Bind(_ *http.Request) error {
	if c.ChannelID == "" {
		c.ChannelID = ChannelKey(c.ChatID, c.ThreadID)
	}
	if c.State == "" {
		c.State = StateActive
	}
	if c.Kind == "" {
		c.Kind = ChannelKind(c.ChatID, c.ThreadID)
	}
	return validate.Struct(c)
}

// ChannelKind guesses the kind from the chat identifier: Telegram group and
// channel identifiers are negative, a thread means a forum topic.
func ChannelKind(chatID int64, threadID int) string {
	if chatID > 0 {
		return ChannelPrivate
	}
	if threadID != 0 {
		return ChannelForum
	}
	return ChannelGroup
}

func (c *Channel) IsActive() bool {
	return c.State == StateActive
}
