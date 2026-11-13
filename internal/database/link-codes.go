package database

import (
	"fmt"
	"go.mongodb.org/mongo-driver/bson"
	"mittere/entity"
	"time"
)

// CreateLinkCode stores a freshly issued chat linking code.
func (m *MongoDB) CreateLinkCode(code *entity.LinkCode) error {
	ctx, cancel := timeoutCtx()
	defer cancel()

	_, err := m.collection(linkCodesCollection).InsertOne(ctx, code)
	return err
}

// GetLinkCode returns a linking code, or nil when there is none.
func (m *MongoDB) GetLinkCode(code string) (*entity.LinkCode, error) {
	ctx, cancel := timeoutCtx()
	defer cancel()

	result := m.collection(linkCodesCollection).FindOne(ctx, bson.M{"code": code})
	if result.Err() != nil {
		return nil, m.findError(result.Err())
	}
	linkCode := &entity.LinkCode{}
	if err := result.Decode(linkCode); err != nil {
		return nil, fmt.Errorf("mongodb decode error: %w", err)
	}
	return linkCode, nil
}

// UseLinkCode marks a code as spent by a channel. It fails when the code was
// already used, so two chats cannot claim the same code.
func (m *MongoDB) UseLinkCode(code, channelID string, usedAt time.Time) error {
	ctx, cancel := timeoutCtx()
	defer cancel()

	filter := bson.M{"code": code, "used_at": bson.M{"$exists": false}, "expires_at": bson.M{"$gt": usedAt}}
	update := bson.M{"$set": bson.M{"used_at": usedAt, "channel_id": channelID}}
	result, err := m.collection(linkCodesCollection).UpdateOne(ctx, filter, update)
	if err != nil {
		return err
	}
	if result.MatchedCount == 0 {
		return fmt.Errorf("code is unknown, expired or already used")
	}
	return nil
}
