package database

import (
	"context"
	"fmt"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/options"
	"mittere/entity"
)

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
