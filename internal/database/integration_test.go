package database

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"mittere/entity"
	"mittere/impl/core"
	"mittere/internal/lib/memrepo"
	"testing"
)

// TestPublishOverMongo runs the whole dispatch over a real database: the topic
// is resolved, the recipients are read, the outcome of every delivery is
// journalled, and a repeated call with the same dedup key only retries what
// failed.
func TestPublishOverMongo(t *testing.T) {
	db := testDB(t)
	prepare(t, db)

	topic := entity.NewTopic("marketing.shipments", "Відправки")
	if err := db.CreateTopic(&topic); err != nil {
		t.Fatalf("CreateTopic() error = %v", err)
	}
	for _, chatID := range []int64{-1001, -1002} {
		channel := entity.NewChannel(chatID, 0, entity.ChannelGroup)
		if err := db.SaveChannel(&channel); err != nil {
			t.Fatalf("SaveChannel() error = %v", err)
		}
		sub := entity.NewSubscription(topic.Code, channel.ChannelID, "test")
		if err := db.Subscribe(&sub); err != nil {
			t.Fatalf("Subscribe() error = %v", err)
		}
	}

	transport := memrepo.NewTransport()
	transport.FailFor[-1002] = errors.New("bot is not a member of the chat")
	handler := core.New(db, slog.New(slog.NewTextHandler(io.Discard, nil)))
	handler.SetTransport(transport)

	msg := &entity.Message{Topic: topic.Code, Type: "alert", Text: "накладна 42", DedupKey: "invoice-42"}
	event, err := handler.Publish(context.Background(), msg, nil)
	if err != nil {
		t.Fatalf("Publish() error = %v", err)
	}
	if event.Status != entity.EventPartial {
		t.Fatalf("status = %q, want %q", event.Status, entity.EventPartial)
	}

	stored, err := db.GetEvent(event.EventID)
	if err != nil || stored == nil {
		t.Fatalf("event not journalled: %v", err)
	}
	if len(stored.Deliveries) != 2 {
		t.Fatalf("journalled deliveries = %d, want 2", len(stored.Deliveries))
	}

	delete(transport.FailFor, -1002)
	retry, err := handler.Publish(context.Background(), msg, nil)
	if err != nil {
		t.Fatalf("retry Publish() error = %v", err)
	}
	if retry.EventID != event.EventID || retry.Status != entity.EventDelivered {
		t.Errorf("retry = %+v, want the same event, now delivered", retry)
	}
	if sent := transport.SentTo(); len(sent) != 2 {
		t.Errorf("transport was asked to send %d times, want 2: %v", len(sent), sent)
	}

	found, err := db.FindEvents(entity.EventFilter{Topic: topic.Code, Status: entity.EventDelivered})
	if err != nil || len(found) != 1 {
		t.Errorf("journal search returned %d events (%v)", len(found), err)
	}
}
