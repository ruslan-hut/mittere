package database

import (
	"errors"
	"mittere/entity"
	"testing"
	"time"
)

// TestEventFeedAndTriage covers what the agent relies on: the feed of its
// topics by a cursor, the triage filter, and verdict and retraction written
// without disturbing each other.
func TestEventFeedAndTriage(t *testing.T) {
	db := testDB(t)
	prepare(t, db)

	base := time.Now().UTC().Truncate(time.Millisecond).Add(-time.Hour)
	events := []entity.Event{
		{EventID: "a1", Topic: "rior.eventlog", CreatedAt: base},
		{EventID: "a2", Topic: "rior.eventlog", CreatedAt: base.Add(time.Minute)},
		{EventID: "a3", Topic: "rior.eventlog", CreatedAt: base.Add(time.Minute)},
		{EventID: "a4", Topic: "rior.eventlog", CreatedAt: base.Add(2 * time.Minute)},
		{EventID: "b1", Topic: "ops", CreatedAt: base.Add(30 * time.Second)},
	}
	for i := range events {
		events[i].Status = entity.EventDelivered
		events[i].Deliveries = []entity.Delivery{{ChannelID: "-1001", ChatID: -1001, Status: entity.DeliverySent, MessageID: i + 1}}
		if err := db.SaveEvent(&events[i]); err != nil {
			t.Fatalf("SaveEvent() error = %v", err)
		}
	}

	triage := &entity.Triage{Verdict: entity.VerdictTask, TaskKey: "T1", By: "hermes", At: time.Now().UTC()}
	if err := db.SetEventTriage("a2", triage); err != nil {
		t.Fatalf("SetEventTriage() error = %v", err)
	}
	if err := db.SetEventTriage("missing", triage); !errors.Is(err, entity.ErrNotFound) {
		t.Errorf("SetEventTriage() on a missing event = %v, want ErrNotFound", err)
	}

	no, yes := false, true
	tests := []struct {
		name   string
		filter entity.EventFilter
		want   []string
	}{
		{"the agent's topics, oldest first",
			entity.EventFilter{Topics: []string{"rior.eventlog"}, Ascending: true}, []string{"a1", "a2", "a3", "a4"}},
		{"untriaged",
			entity.EventFilter{Topics: []string{"rior.eventlog"}, Triaged: &no, Ascending: true}, []string{"a1", "a3", "a4"}},
		{"triaged",
			entity.EventFilter{Topics: []string{"rior.eventlog"}, Triaged: &yes}, []string{"a2"}},
		{"after a cursor sharing its instant",
			entity.EventFilter{Topics: []string{"rior.eventlog"}, AfterTime: base.Add(time.Minute), AfterID: "a2"}, []string{"a3", "a4"}},
		{"after a cursor, untriaged, one page",
			entity.EventFilter{Topics: []string{"rior.eventlog"}, Triaged: &no, AfterTime: base, AfterID: "a1", Limit: 1}, []string{"a3"}},
		{"newest first without a cursor",
			entity.EventFilter{Topics: []string{"rior.eventlog", "ops"}, Limit: 2}, []string{"a4", "a3"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			found, err := db.FindEvents(tt.filter)
			if err != nil {
				t.Fatalf("FindEvents() error = %v", err)
			}
			got := make([]string, 0, len(found))
			for _, event := range found {
				got = append(got, event.EventID)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("events = %v, want %v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("events = %v, want %v", got, tt.want)
				}
			}
		})
	}

	deliveries := []entity.Delivery{{ChannelID: "-1001", ChatID: -1001, Status: entity.DeliveryRetracted, MessageID: 2}}
	retraction := &entity.Retraction{Reason: "duplicate", By: "hermes", At: time.Now().UTC()}
	if err := db.SetEventRetraction("a2", retraction, deliveries); err != nil {
		t.Fatalf("SetEventRetraction() error = %v", err)
	}
	stored, err := db.GetEvent("a2")
	if err != nil || stored == nil {
		t.Fatalf("GetEvent() = %v, %v", stored, err)
	}
	if stored.Retraction == nil || stored.Retraction.Reason != "duplicate" {
		t.Errorf("retraction = %+v", stored.Retraction)
	}
	if stored.Triage == nil || stored.Triage.TaskKey != "T1" {
		t.Errorf("the retraction wiped the verdict: %+v", stored.Triage)
	}
	if stored.Deliveries[0].Status != entity.DeliveryRetracted {
		t.Errorf("delivery status = %s, want retracted", stored.Deliveries[0].Status)
	}
}

// TestEventLease checks that a retraction holds the event against a delivery
// and a verdict, and lets it go when written.
func TestEventLease(t *testing.T) {
	db := testDB(t)
	prepare(t, db)

	event := &entity.Event{EventID: "l1", Topic: "rior.eventlog", DedupKey: "log-1", Status: entity.EventDelivered, CreatedAt: time.Now().UTC()}
	if err := db.SaveEvent(event); err != nil {
		t.Fatalf("SaveEvent() error = %v", err)
	}
	leased, err := db.LeaseEvent("l1", time.Minute)
	if err != nil || leased == nil || !leased.IsLeased(time.Now().UTC()) {
		t.Fatalf("LeaseEvent() = %+v, %v", leased, err)
	}
	if _, err = db.LeaseEvent("l1", time.Minute); !errors.Is(err, entity.ErrInProgress) {
		t.Errorf("second LeaseEvent() = %v, want ErrInProgress", err)
	}
	if _, err = db.LeaseEvent("missing", time.Minute); !errors.Is(err, entity.ErrNotFound) {
		t.Errorf("LeaseEvent() on a missing event = %v, want ErrNotFound", err)
	}
	if err = db.SetEventTriage("l1", &entity.Triage{Verdict: entity.VerdictNoise}); !errors.Is(err, entity.ErrInProgress) {
		t.Errorf("SetEventTriage() under a lease = %v, want ErrInProgress", err)
	}
	retry := &entity.Event{EventID: "l2", Topic: "rior.eventlog", DedupKey: "log-1", CreatedAt: time.Now().UTC()}
	if _, err = db.ClaimEvent(retry, time.Minute); !errors.Is(err, entity.ErrInProgress) {
		t.Errorf("ClaimEvent() of a retry under a lease = %v, want ErrInProgress", err)
	}

	retraction := &entity.Retraction{Reason: "noise", By: "hermes", At: time.Now().UTC()}
	if err = db.SetEventRetraction("l1", retraction, nil); err != nil {
		t.Fatalf("SetEventRetraction() error = %v", err)
	}
	if err = db.SetEventTriage("l1", &entity.Triage{Verdict: entity.VerdictNoise}); err != nil {
		t.Errorf("SetEventTriage() after the lease was released = %v", err)
	}
}
