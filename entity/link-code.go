package entity

import "time"

// LinkCode is a one-off code issued to link a Telegram chat to a topic: the
// code is sent in the chat with the bot's link command, so no chat identifier
// has to be typed by hand and the bot's privacy mode is not in the way.
type LinkCode struct {
	Code      string     `json:"code" bson:"code"`
	Topic     string     `json:"topic,omitempty" bson:"topic"`
	CreatedAt time.Time  `json:"created_at" bson:"created_at"`
	ExpiresAt time.Time  `json:"expires_at" bson:"expires_at"`
	CreatedBy string     `json:"created_by,omitempty" bson:"created_by"`
	UsedAt    *time.Time `json:"used_at,omitempty" bson:"used_at,omitempty"`
	ChannelID string     `json:"channel_id,omitempty" bson:"channel_id,omitempty"`
}

func (c *LinkCode) IsUsable(now time.Time) bool {
	return c.UsedAt == nil && now.Before(c.ExpiresAt)
}
