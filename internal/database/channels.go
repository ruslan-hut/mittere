package database

import (
	"context"
	"fmt"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/options"
	"mittere/entity"
)

// GetChannels returns every known channel.
func (m *MongoDB) GetChannels() ([]entity.Channel, error) {
	ctx, cancel := timeoutCtx()
	defer cancel()

	cursor, err := m.collection(channelsCollection).Find(ctx, bson.D{}, options.Find().SetSort(bson.D{{Key: "channel_id", Value: 1}}))
	if err != nil {
		return nil, err
	}
	channels := make([]entity.Channel, 0)
	if err = cursor.All(ctx, &channels); err != nil {
		return nil, err
	}
	return channels, nil
}

// GetChannel returns a channel by its identifier, or nil when there is none.
func (m *MongoDB) GetChannel(channelID string) (*entity.Channel, error) {
	ctx, cancel := timeoutCtx()
	defer cancel()

	result := m.collection(channelsCollection).FindOne(ctx, bson.M{"channel_id": channelID})
	if result.Err() != nil {
		return nil, m.findError(result.Err())
	}
	channel := &entity.Channel{}
	if err := result.Decode(channel); err != nil {
		return nil, fmt.Errorf("mongodb decode error: %w", err)
	}
	return channel, nil
}

// SaveChannel inserts or updates a channel by its identifier.
func (m *MongoDB) SaveChannel(channel *entity.Channel) error {
	ctx, cancel := timeoutCtx()
	defer cancel()

	filter := bson.M{"channel_id": channel.ChannelID}
	update := bson.M{"$set": channel}
	_, err := m.collection(channelsCollection).UpdateOne(ctx, filter, update, options.Update().SetUpsert(true))
	return err
}

// DeleteChannel removes a channel and all its subscriptions.
func (m *MongoDB) DeleteChannel(channelID string) error {
	ctx, cancel := timeoutCtx()
	defer cancel()

	result, err := m.collection(channelsCollection).DeleteOne(ctx, bson.M{"channel_id": channelID})
	if err != nil {
		return err
	}
	if result.DeletedCount == 0 {
		return fmt.Errorf("channel %s: %w", channelID, entity.ErrNotFound)
	}
	_, err = m.collection(subscriptionsCollection).DeleteMany(ctx, bson.M{"channel_id": channelID})
	return err
}

// GetChannelsByTopic returns active channels subscribed to a topic.
func (m *MongoDB) GetChannelsByTopic(topic string) ([]entity.Channel, error) {
	ctx, cancel := timeoutCtx()
	defer cancel()

	cursor, err := m.collection(subscriptionsCollection).Find(ctx, bson.M{"topic_code": topic})
	if err != nil {
		return nil, err
	}
	var subs []entity.Subscription
	if err = cursor.All(ctx, &subs); err != nil {
		return nil, err
	}
	if len(subs) == 0 {
		return []entity.Channel{}, nil
	}
	ids := make([]string, 0, len(subs))
	for _, sub := range subs {
		ids = append(ids, sub.ChannelID)
	}

	filter := bson.M{"channel_id": bson.M{"$in": ids}, "state": entity.StateActive}
	chCursor, err := m.collection(channelsCollection).Find(ctx, filter, options.Find().SetSort(bson.D{{Key: "channel_id", Value: 1}}))
	if err != nil {
		return nil, err
	}
	channels := make([]entity.Channel, 0, len(ids))
	if err = chCursor.All(ctx, &channels); err != nil {
		return nil, err
	}
	return channels, nil
}

// GetChannelsByUsername returns active channels of a Telegram username; used by
// the legacy endpoint, which targets a person rather than a topic.
func (m *MongoDB) GetChannelsByUsername(username string) ([]entity.Channel, error) {
	ctx, cancel := timeoutCtx()
	defer cancel()

	filter := bson.M{"username": username, "state": entity.StateActive}
	cursor, err := m.collection(channelsCollection).Find(ctx, filter)
	if err != nil {
		return nil, err
	}
	channels := make([]entity.Channel, 0)
	if err = cursor.All(ctx, &channels); err != nil {
		return nil, err
	}
	return channels, nil
}

// CountChannels returns the number of active channels.
func (m *MongoDB) CountChannels(ctx context.Context) (int64, error) {
	return m.collection(channelsCollection).CountDocuments(ctx, bson.M{"state": entity.StateActive})
}
