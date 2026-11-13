package database

import (
	"fmt"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/options"
	"mittere/entity"
)

// GetSenderByToken returns the sender holding an API token, or nil.
func (m *MongoDB) GetSenderByToken(token string) (*entity.Sender, error) {
	ctx, cancel := timeoutCtx()
	defer cancel()

	result := m.collection(sendersCollection).FindOne(ctx, bson.M{"token": token})
	if result.Err() != nil {
		return nil, m.findError(result.Err())
	}
	sender := &entity.Sender{}
	if err := result.Decode(sender); err != nil {
		return nil, fmt.Errorf("mongodb decode error: %w", err)
	}
	return sender, nil
}

// GetSenders returns all senders, ordered by username.
func (m *MongoDB) GetSenders() ([]entity.Sender, error) {
	ctx, cancel := timeoutCtx()
	defer cancel()

	cursor, err := m.collection(sendersCollection).Find(ctx, bson.D{}, options.Find().SetSort(bson.D{{Key: "username", Value: 1}}))
	if err != nil {
		return nil, err
	}
	senders := make([]entity.Sender, 0)
	if err = cursor.All(ctx, &senders); err != nil {
		return nil, err
	}
	return senders, nil
}

// GetSender returns a sender by username, or nil.
func (m *MongoDB) GetSender(username string) (*entity.Sender, error) {
	ctx, cancel := timeoutCtx()
	defer cancel()

	result := m.collection(sendersCollection).FindOne(ctx, bson.M{"username": username})
	if result.Err() != nil {
		return nil, m.findError(result.Err())
	}
	sender := &entity.Sender{}
	if err := result.Decode(sender); err != nil {
		return nil, fmt.Errorf("mongodb decode error: %w", err)
	}
	return sender, nil
}

// CreateSender inserts a sender; the username must be free.
func (m *MongoDB) CreateSender(sender *entity.Sender) error {
	ctx, cancel := timeoutCtx()
	defer cancel()

	_, err := m.collection(sendersCollection).InsertOne(ctx, sender)
	return err
}

// UpdateSender updates a sender by username.
func (m *MongoDB) UpdateSender(sender *entity.Sender) error {
	ctx, cancel := timeoutCtx()
	defer cancel()

	result, err := m.collection(sendersCollection).UpdateOne(ctx, bson.M{"username": sender.Username}, bson.M{"$set": sender})
	if err != nil {
		return err
	}
	if result.MatchedCount == 0 {
		return fmt.Errorf("sender not found")
	}
	return nil
}

// DeleteSender removes a sender by username.
func (m *MongoDB) DeleteSender(username string) error {
	ctx, cancel := timeoutCtx()
	defer cancel()

	result, err := m.collection(sendersCollection).DeleteOne(ctx, bson.M{"username": username})
	if err != nil {
		return err
	}
	if result.DeletedCount == 0 {
		return fmt.Errorf("sender not found")
	}
	return nil
}
