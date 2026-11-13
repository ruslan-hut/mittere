package database

import (
	"fmt"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/options"
	"mittere/entity"
)

// GetTopics returns all topics, ordered by code.
func (m *MongoDB) GetTopics() ([]entity.Topic, error) {
	ctx, cancel := timeoutCtx()
	defer cancel()

	cursor, err := m.collection(topicsCollection).Find(ctx, bson.D{}, options.Find().SetSort(bson.D{{Key: "code", Value: 1}}))
	if err != nil {
		return nil, err
	}
	topics := make([]entity.Topic, 0)
	if err = cursor.All(ctx, &topics); err != nil {
		return nil, err
	}
	return topics, nil
}

// GetTopic returns a topic by code, or nil when there is none.
func (m *MongoDB) GetTopic(code string) (*entity.Topic, error) {
	ctx, cancel := timeoutCtx()
	defer cancel()

	result := m.collection(topicsCollection).FindOne(ctx, bson.M{"code": code})
	if result.Err() != nil {
		return nil, m.findError(result.Err())
	}
	topic := &entity.Topic{}
	if err := result.Decode(topic); err != nil {
		return nil, fmt.Errorf("mongodb decode error: %w", err)
	}
	return topic, nil
}

// CreateTopic inserts a topic; the code must be free.
func (m *MongoDB) CreateTopic(topic *entity.Topic) error {
	ctx, cancel := timeoutCtx()
	defer cancel()

	_, err := m.collection(topicsCollection).InsertOne(ctx, topic)
	return err
}

// EnsureTopic inserts a topic only when its code is not taken.
func (m *MongoDB) EnsureTopic(topic *entity.Topic) error {
	ctx, cancel := timeoutCtx()
	defer cancel()

	filter := bson.M{"code": topic.Code}
	update := bson.M{"$setOnInsert": topic}
	_, err := m.collection(topicsCollection).UpdateOne(ctx, filter, update, options.Update().SetUpsert(true))
	return err
}

// UpdateTopic replaces a topic by code.
func (m *MongoDB) UpdateTopic(topic *entity.Topic) error {
	ctx, cancel := timeoutCtx()
	defer cancel()

	result, err := m.collection(topicsCollection).UpdateOne(ctx, bson.M{"code": topic.Code}, bson.M{"$set": topic})
	if err != nil {
		return err
	}
	if result.MatchedCount == 0 {
		return fmt.Errorf("topic not found")
	}
	return nil
}

// DeleteTopic removes a topic and the subscriptions pointing at it.
func (m *MongoDB) DeleteTopic(code string) error {
	ctx, cancel := timeoutCtx()
	defer cancel()

	result, err := m.collection(topicsCollection).DeleteOne(ctx, bson.M{"code": code})
	if err != nil {
		return err
	}
	if result.DeletedCount == 0 {
		return fmt.Errorf("topic not found")
	}
	_, err = m.collection(subscriptionsCollection).DeleteMany(ctx, bson.M{"topic_code": code})
	return err
}
