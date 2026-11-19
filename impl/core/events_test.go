package core

import (
	"context"
	"errors"
	"mittere/entity"
	"mittere/internal/lib/memrepo"
	"testing"
	"time"
)

func agent(topics ...string) *entity.Sender {
	return &entity.Sender{Username: "hermes", Scope: entity.ScopeAgent, State: entity.StateActive, AllowedTopics: topics}
}

func TestTriageEvent(t *testing.T) {
	future := time.Now().UTC().Add(time.Minute)
	tests := []struct {
		name    string
		event   entity.Event
		triage  entity.Triage
		wantErr error
	}{
		{"task with its key", entity.Event{EventID: "e", Topic: "rior.eventlog"},
			entity.Triage{Verdict: entity.VerdictTask, TaskKey: "T44d6a120"}, nil},
		{"duplicate", entity.Event{EventID: "e", Topic: "rior.eventlog"},
			entity.Triage{Verdict: entity.VerdictDuplicate, Note: "same as yesterday"}, nil},
		{"unknown verdict", entity.Event{EventID: "e", Topic: "rior.eventlog"},
			entity.Triage{Verdict: "later"}, entity.ErrInvalid},
		{"another topic", entity.Event{EventID: "e", Topic: "ops"},
			entity.Triage{Verdict: entity.VerdictNoise}, ErrForbidden},
		{"still being delivered", entity.Event{EventID: "e", Topic: "rior.eventlog", LeaseUntil: &future},
			entity.Triage{Verdict: entity.VerdictNoise}, ErrInProgress},
		{"missing event", entity.Event{EventID: "other", Topic: "rior.eventlog"},
			entity.Triage{Verdict: entity.VerdictNoise}, ErrNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := memrepo.New()
			repo.Events[tt.event.EventID] = tt.event
			c := testCore(repo, memrepo.NewTransport())
			triage := tt.triage
			event, err := c.TriageEvent(agent("rior.eventlog"), "e", &triage)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("TriageEvent() error = %v, want %v", err, tt.wantErr)
			}
			if tt.wantErr != nil {
				return
			}
			stored := repo.Events["e"]
			if stored.Triage == nil || stored.Triage.Verdict != tt.triage.Verdict || stored.Triage.By != "hermes" || stored.Triage.At.IsZero() {
				t.Errorf("stored triage = %+v", stored.Triage)
			}
			if event.Triage == nil || event.Triage.TaskKey != tt.triage.TaskKey {
				t.Errorf("answered triage = %+v", event.Triage)
			}
		})
	}
}

func TestTriageRepeatReplaces(t *testing.T) {
	repo := memrepo.New()
	repo.Events["e"] = entity.Event{EventID: "e", Topic: "rior.eventlog"}
	c := testCore(repo, memrepo.NewTransport())
	if _, err := c.TriageEvent(agent("rior.eventlog"), "e", &entity.Triage{Verdict: entity.VerdictTask, TaskKey: "T1"}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.TriageEvent(agent("rior.eventlog"), "e", &entity.Triage{Verdict: entity.VerdictDuplicate}); err != nil {
		t.Fatal(err)
	}
	if got := repo.Events["e"].Triage; got.Verdict != entity.VerdictDuplicate || got.TaskKey != "" {
		t.Errorf("triage = %+v, want the second verdict only", got)
	}
}

func TestRetractEvent(t *testing.T) {
	sent := func(chatID int64, messageID int) entity.Delivery {
		return entity.Delivery{ChannelID: "c", ChatID: chatID, Status: entity.DeliverySent, MessageID: messageID}
	}
	tests := []struct {
		name        string
		deliveries  []entity.Delivery
		refuse      []int64
		wantStatus  []string
		wantDeleted int
	}{
		{"every message deleted", []entity.Delivery{sent(1, 10), sent(2, 20)}, nil,
			[]string{entity.DeliveryRetracted, entity.DeliveryRetracted}, 2},
		{"telegram refuses one", []entity.Delivery{sent(1, 10), sent(2, 20)}, []int64{2},
			[]string{entity.DeliveryRetracted, entity.DeliverySent}, 1},
		{"failed and unsent deliveries are left alone", []entity.Delivery{
			{ChannelID: "c", ChatID: 1, Status: entity.DeliveryFailed, Error: "blocked"},
			{ChannelID: "c", ChatID: 2, Status: entity.DeliverySent},
			sent(3, 30),
		}, nil, []string{entity.DeliveryFailed, entity.DeliverySent, entity.DeliveryRetracted}, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := memrepo.New()
			repo.Events["e"] = entity.Event{EventID: "e", Topic: "rior.eventlog", Deliveries: tt.deliveries}
			transport := memrepo.NewTransport()
			for _, chatID := range tt.refuse {
				transport.DeleteFailFor[chatID] = errors.New("message can't be deleted for everyone")
			}
			c := testCore(repo, transport)
			if _, err := c.RetractEvent(context.Background(), agent("rior.eventlog"), "e", "noise"); err != nil {
				t.Fatalf("RetractEvent() error = %v", err)
			}
			stored := repo.Events["e"]
			for i, want := range tt.wantStatus {
				if stored.Deliveries[i].Status != want {
					t.Errorf("delivery %d status = %s, want %s", i, stored.Deliveries[i].Status, want)
				}
			}
			for _, chatID := range tt.refuse {
				for _, delivery := range stored.Deliveries {
					if delivery.ChatID == chatID && delivery.Error == "" {
						t.Errorf("the refused deletion in chat %d does not say why", chatID)
					}
				}
			}
			if transport.DeletedCount() != tt.wantDeleted {
				t.Errorf("deleted %d, want %d", transport.DeletedCount(), tt.wantDeleted)
			}
			if stored.Retraction == nil || stored.Retraction.Reason != "noise" || stored.Retraction.At.IsZero() {
				t.Errorf("retraction = %+v", stored.Retraction)
			}

			// a repeat deletes nothing twice and retries only what was refused
			transport.DeleteFailFor = map[int64]error{}
			if _, err := c.RetractEvent(context.Background(), agent("rior.eventlog"), "e", "noise"); err != nil {
				t.Fatalf("repeated RetractEvent() error = %v", err)
			}
			if transport.DeletedCount() != tt.wantDeleted+len(tt.refuse) {
				t.Errorf("after the repeat deleted %d, want %d", transport.DeletedCount(), tt.wantDeleted+len(tt.refuse))
			}
		})
	}
}

// TestRetractedEventIsNotSentAgain checks that a retry of 1C with the same
// dedup key does not bring a retracted message back into the chat.
func TestRetractedEventIsNotSentAgain(t *testing.T) {
	repo := memrepo.New().WithTopic("rior.eventlog", 111, 222)
	transport := memrepo.NewTransport()
	transport.FailFor[222] = errors.New("chat not found")
	c := testCore(repo, transport)
	msg := &entity.Message{Topic: "rior.eventlog", Type: "error", DedupKey: "log-1"}

	event, err := c.Publish(context.Background(), msg, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = c.TriageEvent(agent("rior.eventlog"), event.EventID, &entity.Triage{Verdict: entity.VerdictNoise}); err != nil {
		t.Fatal(err)
	}
	if _, err = c.RetractEvent(context.Background(), agent("rior.eventlog"), event.EventID, "noise"); err != nil {
		t.Fatal(err)
	}

	delete(transport.FailFor, 222)
	again, err := c.Publish(context.Background(), msg, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(transport.SentTo()) != 1 {
		t.Errorf("sent to %v, want the first delivery only", transport.SentTo())
	}
	if again.Retraction == nil || again.Triage == nil {
		t.Errorf("the retry lost the retraction or the verdict: %+v", again)
	}
}

// TestRetractHoldsTheEvent checks that a retry from 1C arriving while the
// messages are being deleted is turned away, and that a repeated retraction
// keeps the first reason.
func TestRetractHoldsTheEvent(t *testing.T) {
	repo := memrepo.New().WithTopic("rior.eventlog", 111)
	c := testCore(repo, memrepo.NewTransport())
	msg := &entity.Message{Topic: "rior.eventlog", Type: "error", DedupKey: "log-1"}
	event, err := c.Publish(context.Background(), msg, nil)
	if err != nil {
		t.Fatal(err)
	}

	if _, err = repo.LeaseEvent(event.EventID, time.Minute); err != nil {
		t.Fatal(err)
	}
	if _, err = c.RetractEvent(context.Background(), agent("rior.eventlog"), event.EventID, "noise"); !errors.Is(err, ErrInProgress) {
		t.Errorf("RetractEvent() of a held event = %v, want ErrInProgress", err)
	}
	if err = repo.ReleaseEvent(event.EventID); err != nil {
		t.Fatal(err)
	}

	first, err := c.RetractEvent(context.Background(), agent("rior.eventlog"), event.EventID, "noise")
	if err != nil {
		t.Fatal(err)
	}
	second, err := c.RetractEvent(context.Background(), agent("rior.eventlog"), event.EventID, "changed my mind")
	if err != nil {
		t.Fatal(err)
	}
	if second.Retraction.Reason != "noise" || !second.Retraction.At.Equal(first.Retraction.At) {
		t.Errorf("retraction = %+v, want the first one kept", second.Retraction)
	}
	if stored := repo.Events[event.EventID]; stored.IsLeased(time.Now().UTC()) {
		t.Error("the retraction did not release the event")
	}
}
