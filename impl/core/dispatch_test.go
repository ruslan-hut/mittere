package core

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"mittere/entity"
	"mittere/internal/lib/memrepo"
	"testing"
	"time"
)

func testCore(repo *memrepo.Repo, transport Transport) *Core {
	c := New(repo, slog.New(slog.NewTextHandler(io.Discard, nil)))
	c.SetTransport(transport)
	return c
}

func TestResolveTopic(t *testing.T) {
	tests := []struct {
		name string
		msg  entity.Message
		want string
	}{
		{"explicit topic", entity.Message{Topic: "marketing.shipments"}, "marketing.shipments"},
		{"legacy without role goes to default", entity.Message{}, entity.TopicDefault},
		{"legacy user role goes to default", entity.Message{Role: entity.RoleUser}, entity.TopicDefault},
		{"legacy admin role goes to admin topic", entity.Message{Role: entity.RoleAdmin}, entity.TopicAdmin},
		{"explicit topic wins over role", entity.Message{Topic: "ops", Role: entity.RoleAdmin}, "ops"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ResolveTopic(&tt.msg); got != tt.want {
				t.Errorf("ResolveTopic() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestPublishRouting(t *testing.T) {
	tests := []struct {
		name       string
		setup      func(*memrepo.Repo)
		msg        entity.Message
		wantChats  []int64
		wantStatus string
		wantErr    error
	}{
		{
			name:       "topic reaches its channels only",
			setup:      func(r *memrepo.Repo) { r.WithTopic("marketing.shipments", -100, -200).WithTopic("ops", 555) },
			msg:        entity.Message{Topic: "marketing.shipments", Type: "alert"},
			wantChats:  []int64{-200, -100},
			wantStatus: entity.EventDelivered,
		},
		{
			name:       "legacy message without topic reaches the default topic",
			setup:      func(r *memrepo.Repo) { r.WithTopic(entity.TopicDefault, 111).WithTopic("ops", 222) },
			msg:        entity.Message{Type: "alert"},
			wantChats:  []int64{111},
			wantStatus: entity.EventDelivered,
		},
		{
			name:       "legacy admin role reaches the admin topic",
			setup:      func(r *memrepo.Repo) { r.WithTopic(entity.TopicDefault, 111).WithTopic(entity.TopicAdmin, 999) },
			msg:        entity.Message{Type: "alert", Role: entity.RoleAdmin},
			wantChats:  []int64{999},
			wantStatus: entity.EventDelivered,
		},
		{
			name: "legacy username targeting ignores the topic",
			setup: func(r *memrepo.Repo) {
				r.WithTopic(entity.TopicDefault, 111, 222)
				channel := r.Channels[entity.ChannelKey(222, 0)]
				channel.Username = "johndoe"
				r.Channels[channel.ChannelID] = channel
			},
			msg:        entity.Message{Type: "alert", Username: "johndoe"},
			wantChats:  []int64{222},
			wantStatus: entity.EventDelivered,
		},
		{
			name:       "disabled channel is skipped",
			setup:      func(r *memrepo.Repo) { r.WithTopic("ops", 111, 222); disable(r, 111) },
			msg:        entity.Message{Topic: "ops", Type: "alert"},
			wantChats:  []int64{222},
			wantStatus: entity.EventDelivered,
		},
		{
			name:       "topic without channels is reported, not silently dropped",
			setup:      func(r *memrepo.Repo) { r.WithTopic("ops") },
			msg:        entity.Message{Topic: "ops", Type: "alert"},
			wantChats:  []int64{},
			wantStatus: entity.EventNoRecipients,
		},
		{
			name:    "unknown topic is refused",
			setup:   func(r *memrepo.Repo) { r.WithTopic("ops", 111) },
			msg:     entity.Message{Topic: "marketing.shipments", Type: "alert"},
			wantErr: ErrUnknownTopic,
		},
		{
			name:    "malformed topic code is refused",
			setup:   func(r *memrepo.Repo) { r.WithTopic("ops", 111) },
			msg:     entity.Message{Topic: "Marketing Shipments", Type: "alert"},
			wantErr: ErrUnknownTopic,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := memrepo.New()
			tt.setup(repo)
			transport := memrepo.NewTransport()
			c := testCore(repo, transport)

			event, err := c.Publish(context.Background(), &tt.msg, nil)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("Publish() error = %v, want %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Publish() error = %v", err)
			}
			if event.Status != tt.wantStatus {
				t.Errorf("status = %q, want %q", event.Status, tt.wantStatus)
			}
			if !sameChats(transport.SentTo(), tt.wantChats) {
				t.Errorf("sent to %v, want %v", transport.SentTo(), tt.wantChats)
			}
			if len(event.Deliveries) != len(tt.wantChats) {
				t.Errorf("deliveries = %d, want %d", len(event.Deliveries), len(tt.wantChats))
			}
		})
	}
}

// TestPublishReportsEveryDelivery is the point of the rework: the caller is
// told what reached whom instead of being told the message was accepted.
func TestPublishReportsEveryDelivery(t *testing.T) {
	repo := memrepo.New().WithTopic("ops", 111, 222, 333)
	transport := memrepo.NewTransport()
	transport.FailFor[222] = errors.New("bot is not a member of the chat")
	c := testCore(repo, transport)

	event, err := c.Publish(context.Background(), &entity.Message{Topic: "ops", Type: "alert"}, nil)
	if err != nil {
		t.Fatalf("Publish() error = %v", err)
	}
	if event.Status != entity.EventPartial {
		t.Fatalf("status = %q, want %q", event.Status, entity.EventPartial)
	}

	byChat := make(map[int64]entity.Delivery, len(event.Deliveries))
	for _, delivery := range event.Deliveries {
		byChat[delivery.ChatID] = delivery
	}
	if got := byChat[222]; got.Status != entity.DeliveryFailed || got.Error == "" {
		t.Errorf("chat 222 = %+v, want a failed delivery carrying the reason", got)
	}
	for _, chatID := range []int64{111, 333} {
		if got := byChat[chatID]; got.Status != entity.DeliverySent || got.MessageID == 0 {
			t.Errorf("chat %d = %+v, want a sent delivery with a message id", chatID, got)
		}
	}

	stored, err := repo.GetEvent(event.EventID)
	if err != nil || stored == nil {
		t.Fatalf("event not journalled: %v", err)
	}
	if stored.Status != entity.EventPartial || len(stored.Deliveries) != 3 {
		t.Errorf("journalled event = %+v, want partial with 3 deliveries", stored)
	}
}

func TestPublishAllFailed(t *testing.T) {
	repo := memrepo.New().WithTopic("ops", 111, 222)
	transport := memrepo.NewTransport()
	transport.FailFor[111] = errors.New("chat not found")
	transport.FailFor[222] = errors.New("chat not found")
	c := testCore(repo, transport)

	event, err := c.Publish(context.Background(), &entity.Message{Topic: "ops", Type: "alert"}, nil)
	if err != nil {
		t.Fatalf("Publish() error = %v", err)
	}
	if event.Status != entity.EventFailed {
		t.Errorf("status = %q, want %q", event.Status, entity.EventFailed)
	}
}

// TestPublishDedupRetriesOnlyWhatFailed covers the retry 1C makes after a
// non-200 answer: the recipients that got the message must not get it twice.
func TestPublishDedupRetriesOnlyWhatFailed(t *testing.T) {
	repo := memrepo.New().WithTopic("ops", 111, 222)
	transport := memrepo.NewTransport()
	transport.FailFor[222] = errors.New("timeout")
	c := testCore(repo, transport)

	msg := entity.Message{Topic: "ops", Type: "alert", DedupKey: "invoice-42"}
	first, err := c.Publish(context.Background(), &msg, nil)
	if err != nil {
		t.Fatalf("Publish() error = %v", err)
	}
	if first.Status != entity.EventPartial {
		t.Fatalf("first status = %q, want %q", first.Status, entity.EventPartial)
	}

	delete(transport.FailFor, 222)
	second, err := c.Publish(context.Background(), &msg, nil)
	if err != nil {
		t.Fatalf("second Publish() error = %v", err)
	}
	if second.EventID != first.EventID {
		t.Errorf("event id = %q, want the first one %q", second.EventID, first.EventID)
	}
	if second.Status != entity.EventDelivered {
		t.Errorf("second status = %q, want %q", second.Status, entity.EventDelivered)
	}
	if got := transport.SentTo(); !sameChats(got, []int64{111, 222}) {
		t.Errorf("sent to %v, want 111 once and 222 once", got)
	}

	third, err := c.Publish(context.Background(), &msg, nil)
	if err != nil {
		t.Fatalf("third Publish() error = %v", err)
	}
	if third.EventID != first.EventID {
		t.Errorf("third event id = %q, want %q", third.EventID, first.EventID)
	}
	if got := transport.SentTo(); len(got) != 2 {
		t.Errorf("a delivered event was sent again: %v", got)
	}
}

func TestPublishSenderScope(t *testing.T) {
	tests := []struct {
		name    string
		sender  entity.Sender
		topic   string
		wantErr error
	}{
		{
			name:   "allowed topic",
			sender: entity.Sender{Username: "1c", Scope: entity.ScopeSend, State: entity.StateActive, AllowedTopics: []string{"ops"}},
			topic:  "ops",
		},
		{
			name:    "topic outside the list",
			sender:  entity.Sender{Username: "1c", Scope: entity.ScopeSend, State: entity.StateActive, AllowedTopics: []string{"marketing.shipments"}},
			topic:   "ops",
			wantErr: ErrForbidden,
		},
		{
			name:   "empty list means every topic",
			sender: entity.Sender{Username: "1c", Scope: entity.ScopeSend, State: entity.StateActive},
			topic:  "ops",
		},
		{
			name:   "admin may publish anywhere",
			sender: entity.Sender{Username: "system", Scope: entity.ScopeAdmin, State: entity.StateActive},
			topic:  "ops",
		},
		{
			name:    "disabled sender may not publish",
			sender:  entity.Sender{Username: "1c", Scope: entity.ScopeSend, State: entity.StateDisabled},
			topic:   "ops",
			wantErr: ErrForbidden,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := memrepo.New().WithTopic("ops", 111)
			c := testCore(repo, memrepo.NewTransport())
			_, err := c.Publish(context.Background(), &entity.Message{Topic: tt.topic, Type: "alert"}, &tt.sender)
			if !errors.Is(err, tt.wantErr) && !(tt.wantErr == nil && err == nil) {
				t.Errorf("Publish() error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func disable(r *memrepo.Repo, chatID int64) {
	channel := r.Channels[entity.ChannelKey(chatID, 0)]
	channel.State = entity.StateDisabled
	r.Channels[channel.ChannelID] = channel
}

func sameChats(got, want []int64) bool {
	if len(got) != len(want) {
		return false
	}
	counts := make(map[int64]int, len(want))
	for _, id := range want {
		counts[id]++
	}
	for _, id := range got {
		counts[id]--
	}
	for _, left := range counts {
		if left != 0 {
			return false
		}
	}
	return true
}

// TestPublishRefusesParallelDuplicate covers the retry 1C makes after its own
// timeout while the first call is still fanning out: the message must not go
// out twice.
func TestPublishRefusesParallelDuplicate(t *testing.T) {
	repo := memrepo.New().WithTopic("ops", 111)
	c := testCore(repo, memrepo.NewTransport())

	msg := entity.Message{Topic: "ops", Type: "alert", DedupKey: "invoice-42"}
	running := &entity.Event{EventID: "running", Topic: "ops", DedupKey: "invoice-42", Status: entity.EventPending}
	if _, err := repo.ClaimEvent(running, time.Minute); err != nil {
		t.Fatalf("ClaimEvent() error = %v", err)
	}

	if _, err := c.Publish(context.Background(), &msg, nil); !errors.Is(err, ErrInProgress) {
		t.Errorf("Publish() error = %v, want %v", err, ErrInProgress)
	}
}

// TestPublishResumesAfterCrash covers a service that died mid fan out: the
// deliveries that did go through were journalled, so the retry only sends the
// rest.
func TestPublishResumesAfterCrash(t *testing.T) {
	repo := memrepo.New().WithTopic("ops", 111, 222)
	transport := memrepo.NewTransport()

	sentAt := time.Now().UTC().Add(-time.Minute)
	expired := time.Now().UTC().Add(-time.Second)
	crashed := entity.Event{
		EventID:    "crashed",
		Topic:      "ops",
		DedupKey:   "invoice-42",
		Status:     entity.EventPending,
		LeaseUntil: &expired,
		Deliveries: []entity.Delivery{
			{ChannelID: entity.ChannelKey(111, 0), ChatID: 111, Status: entity.DeliverySent, Attempts: 1, MessageID: 7, SentAt: &sentAt},
			{ChannelID: entity.ChannelKey(222, 0), ChatID: 222, Status: entity.DeliveryPending},
		},
	}
	repo.Events["crashed"] = crashed

	c := testCore(repo, transport)
	event, err := c.Publish(context.Background(), &entity.Message{Topic: "ops", Type: "alert", DedupKey: "invoice-42"}, nil)
	if err != nil {
		t.Fatalf("Publish() error = %v", err)
	}
	if event.EventID != "crashed" || event.Status != entity.EventDelivered {
		t.Errorf("event = %+v, want the same event, now delivered", event)
	}
	if sent := transport.SentTo(); !sameChats(sent, []int64{222}) {
		t.Errorf("sent to %v, want only the recipient that was still pending", sent)
	}
}

// TestPublishKeepsDeliveriesOfChannelsThatLeft makes sure a retry does not
// erase the record of a message that did go out to a channel which has since
// been unsubscribed.
func TestPublishKeepsDeliveriesOfChannelsThatLeft(t *testing.T) {
	repo := memrepo.New().WithTopic("ops", 111, 222)
	transport := memrepo.NewTransport()
	transport.FailFor[222] = errors.New("timeout")
	c := testCore(repo, transport)

	msg := entity.Message{Topic: "ops", Type: "alert", DedupKey: "invoice-42"}
	if _, err := c.Publish(context.Background(), &msg, nil); err != nil {
		t.Fatalf("Publish() error = %v", err)
	}

	delete(repo.Subs["ops"], entity.ChannelKey(111, 0))
	delete(transport.FailFor, 222)

	event, err := c.Publish(context.Background(), &msg, nil)
	if err != nil {
		t.Fatalf("second Publish() error = %v", err)
	}
	if len(event.Deliveries) != 2 {
		t.Fatalf("deliveries = %d, want the new one and the one already sent", len(event.Deliveries))
	}
	if sent := transport.SentTo(); !sameChats(sent, []int64{111, 222}) {
		t.Errorf("sent to %v, want each chat once", sent)
	}
}
