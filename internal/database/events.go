package database

import (
	"context"
	"errors"
	"fmt"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"mittere/entity"
	"time"
)

// ClaimEvent takes the event for delivery and returns what an earlier call
// with the same dedup key left behind, or nil when the event is new. While a
// claim holds, a second call with the same dedup key gets entity.ErrInProgress
// instead of sending the message a second time; a claim older than the lease is
// taken over, so a crashed call does not block the key forever. An event
// without a dedup key is simply inserted: there is nothing to deduplicate.
func (m *MongoDB) ClaimEvent(event *entity.Event, lease time.Duration) (*entity.Event, error) {
	ctx, cancel := timeoutCtx()
	defer cancel()

	now := time.Now().UTC()
	until := now.Add(lease)
	event.LeaseUntil = &until

	if event.DedupKey == "" {
		if _, err := m.collection(eventsCollection).InsertOne(ctx, event); err != nil {
			return nil, err
		}
		return nil, nil
	}

	filter := bson.M{
		"dedup_key": event.DedupKey,
		"$or": []bson.M{
			{"lease_until": bson.M{"$exists": false}},
			{"lease_until": bson.M{"$lt": now}},
		},
	}
	insert, err := insertDocument(event)
	if err != nil {
		return nil, err
	}
	update := bson.M{
		"$set":         bson.M{"lease_until": until},
		"$setOnInsert": insert,
	}
	opts := options.FindOneAndUpdate().SetUpsert(true).SetReturnDocument(options.Before)

	result := m.collection(eventsCollection).FindOneAndUpdate(ctx, filter, update, opts)
	if err = result.Err(); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			// the upsert inserted this event, there was nothing before
			return nil, nil
		}
		if mongo.IsDuplicateKeyError(err) {
			// a document with this key exists but its claim is still valid
			return nil, entity.ErrInProgress
		}
		return nil, fmt.Errorf("mongodb claim error: %w", err)
	}
	previous := &entity.Event{}
	if err = result.Decode(previous); err != nil {
		return nil, fmt.Errorf("mongodb decode error: %w", err)
	}
	return previous, nil
}

// insertDocument renders an event for $setOnInsert, without the lease, which
// the same update sets on its own.
func insertDocument(event *entity.Event) (bson.M, error) {
	raw, err := bson.Marshal(event)
	if err != nil {
		return nil, err
	}
	doc := bson.M{}
	if err = bson.Unmarshal(raw, &doc); err != nil {
		return nil, err
	}
	delete(doc, "lease_until")
	return doc, nil
}

// SaveEvent inserts or replaces the journal record of an event.
func (m *MongoDB) SaveEvent(event *entity.Event) error {
	ctx, cancel := timeoutCtx()
	defer cancel()

	filter := bson.M{"event_id": event.EventID}
	_, err := m.collection(eventsCollection).ReplaceOne(ctx, filter, event, options.Replace().SetUpsert(true))
	return err
}

// GetEvent returns one journal record by its identifier, or nil.
func (m *MongoDB) GetEvent(eventID string) (*entity.Event, error) {
	ctx, cancel := timeoutCtx()
	defer cancel()

	result := m.collection(eventsCollection).FindOne(ctx, bson.M{"event_id": eventID})
	if result.Err() != nil {
		return nil, m.findError(result.Err())
	}
	event := &entity.Event{}
	if err := result.Decode(event); err != nil {
		return nil, fmt.Errorf("mongodb decode error: %w", err)
	}
	return event, nil
}

// GetEventByDedupKey returns the event previously published under a dedup key.
func (m *MongoDB) GetEventByDedupKey(key string) (*entity.Event, error) {
	ctx, cancel := timeoutCtx()
	defer cancel()

	result := m.collection(eventsCollection).FindOne(ctx, bson.M{"dedup_key": key})
	if result.Err() != nil {
		return nil, m.findError(result.Err())
	}
	event := &entity.Event{}
	if err := result.Decode(event); err != nil {
		return nil, fmt.Errorf("mongodb decode error: %w", err)
	}
	return event, nil
}

// FindEvents returns journal records matching a filter, newest first.
func (m *MongoDB) FindEvents(f entity.EventFilter) ([]entity.Event, error) {
	ctx, cancel := timeoutCtx()
	defer cancel()

	filter := bson.M{}
	if f.Topic != "" {
		filter["topic"] = f.Topic
	}
	if f.Status != "" {
		filter["status"] = f.Status
	}
	created := bson.M{}
	if !f.From.IsZero() {
		created["$gte"] = f.From
	}
	if !f.To.IsZero() {
		created["$lte"] = f.To
	}
	if len(created) > 0 {
		filter["created_at"] = created
	}
	limit := f.Limit
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	opts := options.Find().SetSort(bson.D{{Key: "created_at", Value: -1}}).SetLimit(limit)
	cursor, err := m.collection(eventsCollection).Find(ctx, filter, opts)
	if err != nil {
		return nil, err
	}
	events := make([]entity.Event, 0)
	if err = cursor.All(ctx, &events); err != nil {
		return nil, err
	}
	return events, nil
}

// CountFailedEvents returns how many events since a point in time ended with at
// least one failed delivery.
func (m *MongoDB) CountFailedEvents(ctx context.Context, since interface{}) (int64, error) {
	filter := bson.M{
		"created_at": bson.M{"$gte": since},
		"status":     bson.M{"$in": []string{entity.EventFailed, entity.EventPartial}},
	}
	return m.collection(eventsCollection).CountDocuments(ctx, filter)
}
