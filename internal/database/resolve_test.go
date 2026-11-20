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
	"time"
)

// TestResolveOverMongo runs POST /events/resolve over a real database: the
// events of a task are found by its key, the outcome goes once per chat, the
// events are retracted and resolved, and a repeat changes nothing.
func TestResolveOverMongo(t *testing.T) {
	db := testDB(t)
	prepare(t, db)

	base := time.Now().UTC().Truncate(time.Millisecond).Add(-time.Hour)
	events := []entity.Event{
		{EventID: "r1", Topic: "rior.eventlog", CreatedAt: base},
		{EventID: "r2", Topic: "rior.eventlog", CreatedAt: base.Add(time.Minute)},
		{EventID: "r3", Topic: "rior.eventlog", CreatedAt: base.Add(2 * time.Minute)},
	}
	for i := range events {
		events[i].Status = entity.EventDelivered
		events[i].Deliveries = []entity.Delivery{
			{ChannelID: "-1001", ChatID: -1001, Status: entity.DeliverySent, MessageID: 10 + i},
			{ChannelID: "-1002", ChatID: -1002, Status: entity.DeliverySent, MessageID: 20 + i},
		}
		if err := db.SaveEvent(&events[i]); err != nil {
			t.Fatalf("SaveEvent() error = %v", err)
		}
	}
	for id, key := range map[string]string{"r1": "T1", "r2": "T1", "r3": "T2"} {
		triage := &entity.Triage{Verdict: entity.VerdictTask, TaskKey: key, By: "hermes", At: time.Now().UTC()}
		if err := db.SetEventTriage(id, triage); err != nil {
			t.Fatalf("SetEventTriage() error = %v", err)
		}
	}

	found, err := db.FindEvents(entity.EventFilter{TaskKey: "T1", Ascending: true})
	if err != nil {
		t.Fatalf("FindEvents() error = %v", err)
	}
	if len(found) != 2 || found[0].EventID != "r1" || found[1].EventID != "r2" {
		t.Fatalf("events of T1 = %+v, want r1 and r2", found)
	}

	transport := memrepo.NewTransport()
	handler := core.New(db, slog.New(slog.NewTextHandler(io.Discard, nil)))
	handler.SetTransport(transport)
	agent := &entity.Sender{Username: "hermes", Scope: entity.ScopeAgent, State: entity.StateActive, AllowedTopics: []string{"rior.eventlog"}}

	outcome, err := handler.ResolveTask(context.Background(), agent, "T1", "", "fixed in 1C")
	if err != nil {
		t.Fatalf("ResolveTask() error = %v", err)
	}
	if !outcome.IsComplete() {
		t.Fatalf("outcome is not complete: %+v", outcome)
	}
	if len(transport.Messages) != 2 || transport.DeletedCount() != 4 {
		t.Errorf("sent %d, deleted %d, want 2 and 4", len(transport.Messages), transport.DeletedCount())
	}

	result, err := db.GetEvent(outcome.Result.EventID)
	if err != nil || result == nil {
		t.Fatalf("GetEvent() of the result = %v, %v", result, err)
	}
	if result.Resolves != "T1" || result.Triage == nil || result.Triage.TaskKey != "T1" || result.Status != entity.EventDelivered {
		t.Errorf("result = %+v", result)
	}
	for _, id := range []string{"r1", "r2"} {
		stored, getErr := db.GetEvent(id)
		if getErr != nil || stored == nil {
			t.Fatalf("GetEvent(%s) = %v, %v", id, stored, getErr)
		}
		if stored.Resolution == nil || stored.Resolution.ResultID != result.EventID || stored.Retraction == nil {
			t.Errorf("%s: resolution = %+v, retraction = %+v", id, stored.Resolution, stored.Retraction)
		}
		if stored.Triage == nil || stored.Triage.TaskKey != "T1" {
			t.Errorf("%s: the resolution wiped the verdict: %+v", id, stored.Triage)
		}
	}
	if other, _ := db.GetEvent("r3"); other.Resolution != nil || other.Retraction != nil {
		t.Errorf("event of another task was touched: %+v", other)
	}

	if _, err = handler.ResolveTask(context.Background(), agent, "T1", "", "fixed in 1C"); err != nil {
		t.Fatalf("repeated ResolveTask() error = %v", err)
	}
	if len(transport.Messages) != 2 || transport.DeletedCount() != 4 {
		t.Errorf("the repeat sent or deleted again: sent %d, deleted %d", len(transport.Messages), transport.DeletedCount())
	}
}

// TestEventResolutionLease checks that a resolution is not written into an
// event someone holds.
func TestEventResolutionLease(t *testing.T) {
	db := testDB(t)
	prepare(t, db)

	event := &entity.Event{EventID: "s1", Topic: "rior.eventlog", Status: entity.EventDelivered, CreatedAt: time.Now().UTC()}
	if err := db.SaveEvent(event); err != nil {
		t.Fatalf("SaveEvent() error = %v", err)
	}
	resolution := &entity.Resolution{TaskKey: "T1", ResultID: "x", By: "hermes", At: time.Now().UTC()}
	if err := db.SetEventResolution("missing", resolution); !errors.Is(err, entity.ErrNotFound) {
		t.Errorf("SetEventResolution() on a missing event = %v, want ErrNotFound", err)
	}
	if _, err := db.LeaseEvent("s1", time.Minute); err != nil {
		t.Fatalf("LeaseEvent() error = %v", err)
	}
	if err := db.SetEventResolution("s1", resolution); !errors.Is(err, entity.ErrInProgress) {
		t.Errorf("SetEventResolution() under a lease = %v, want ErrInProgress", err)
	}
	if err := db.ReleaseEvent("s1"); err != nil {
		t.Fatalf("ReleaseEvent() error = %v", err)
	}
	if err := db.SetEventResolution("s1", resolution); err != nil {
		t.Fatalf("SetEventResolution() error = %v", err)
	}
	stored, err := db.GetEvent("s1")
	if err != nil || stored.Resolution == nil || stored.Resolution.ResultID != "x" {
		t.Errorf("stored = %+v, %v", stored, err)
	}
}
