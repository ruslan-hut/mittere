package entity

import "time"

// Subscription links a topic to a channel; a channel may hold many topics and a
// topic many channels.
type Subscription struct {
	TopicCode string    `json:"topic" bson:"topic_code"`
	ChannelID string    `json:"channel_id" bson:"channel_id"`
	CreatedAt time.Time `json:"created_at,omitempty" bson:"created_at"`
	CreatedBy string    `json:"created_by,omitempty" bson:"created_by"`
}

func NewSubscription(topic, channelID, by string) Subscription {
	return Subscription{
		TopicCode: topic,
		ChannelID: channelID,
		CreatedAt: time.Now().UTC(),
		CreatedBy: by,
	}
}
