package database

import (
	"fmt"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/options"
	"mittere/entity"
)

// Subscribe links a channel to a topic; linking twice is not an error.
func (m *MongoDB) Subscribe(sub *entity.Subscription) error {
	ctx, cancel := timeoutCtx()
	defer cancel()

	filter := bson.M{"topic_code": sub.TopicCode, "channel_id": sub.ChannelID}
	update := bson.M{"$setOnInsert": sub}
	_, err := m.collection(subscriptionsCollection).UpdateOne(ctx, filter, update, options.Update().SetUpsert(true))
	return err
}

// Unsubscribe removes the link between a channel and a topic.
func (m *MongoDB) Unsubscribe(topic, channelID string) error {
	ctx, cancel := timeoutCtx()
	defer cancel()

	result, err := m.collection(subscriptionsCollection).DeleteOne(ctx, bson.M{"topic_code": topic, "channel_id": channelID})
	if err != nil {
		return err
	}
	if result.DeletedCount == 0 {
		return fmt.Errorf("subscription not found")
	}
	return nil
}

// GetSubscriptions returns every topic-to-channel link.
func (m *MongoDB) GetSubscriptions() ([]entity.Subscription, error) {
	ctx, cancel := timeoutCtx()
	defer cancel()

	cursor, err := m.collection(subscriptionsCollection).Find(ctx, bson.D{})
	if err != nil {
		return nil, err
	}
	subs := make([]entity.Subscription, 0)
	if err = cursor.All(ctx, &subs); err != nil {
		return nil, err
	}
	return subs, nil
}

// GetTopicsOfChannel returns the topic codes a channel is subscribed to.
func (m *MongoDB) GetTopicsOfChannel(channelID string) ([]string, error) {
	ctx, cancel := timeoutCtx()
	defer cancel()

	cursor, err := m.collection(subscriptionsCollection).Find(ctx, bson.M{"channel_id": channelID})
	if err != nil {
		return nil, err
	}
	var subs []entity.Subscription
	if err = cursor.All(ctx, &subs); err != nil {
		return nil, err
	}
	topics := make([]string, 0, len(subs))
	for _, sub := range subs {
		topics = append(topics, sub.TopicCode)
	}
	return topics, nil
}

// SetTopicsOfChannel replaces the subscriptions of a channel.
func (m *MongoDB) SetTopicsOfChannel(channelID string, topics []string, by string) error {
	ctx, cancel := timeoutCtx()
	defer cancel()

	if _, err := m.collection(subscriptionsCollection).DeleteMany(ctx, bson.M{"channel_id": channelID}); err != nil {
		return err
	}
	if len(topics) == 0 {
		return nil
	}
	docs := make([]interface{}, 0, len(topics))
	seen := make(map[string]bool, len(topics))
	for _, topic := range topics {
		if seen[topic] {
			continue
		}
		seen[topic] = true
		sub := entity.NewSubscription(topic, channelID, by)
		docs = append(docs, sub)
	}
	_, err := m.collection(subscriptionsCollection).InsertMany(ctx, docs)
	return err
}
