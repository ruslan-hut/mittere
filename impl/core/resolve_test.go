package core

import (
	"context"
	"errors"
	"mittere/entity"
	"mittere/internal/lib/memrepo"
	"testing"
	"time"
)

// triaged is an event of rior.eventlog triaged to a task and delivered to the
// given chats, one channel per chat.
func triaged(eventID, taskKey string, chats ...int64) entity.Event {
	event := entity.Event{
		EventID:   eventID,
		Topic:     "rior.eventlog",
		Status:    entity.EventDelivered,
		CreatedAt: time.Now().UTC(),
		Triage:    &entity.Triage{Verdict: entity.VerdictTask, TaskKey: taskKey, By: "hermes"},
	}
	for i, chatID := range chats {
		event.Deliveries = append(event.Deliveries, entity.Delivery{
			ChannelID: entity.ChannelKey(chatID, 0),
			ChatID:    chatID,
			Status:    entity.DeliverySent,
			MessageID: 100*int(chatID) + i,
		})
	}
	return event
}

func TestResolveTaskErrors(t *testing.T) {
	future := time.Now().UTC().Add(time.Minute)
	leased := triaged("e1", "T1", 1)
	leased.LeaseUntil = &future
	foreign := triaged("e1", "T1", 1)
	foreign.Topic = "ops"
	tests := []struct {
		name    string
		events  []entity.Event
		taskKey string
		text    string
		wantErr error
	}{
		{"no task key", []entity.Event{triaged("e1", "T1", 1)}, " ", "done", entity.ErrInvalid},
		{"no text", []entity.Event{triaged("e1", "T1", 1)}, "T1", "", entity.ErrInvalid},
		{"no events of the task", []entity.Event{triaged("e1", "T2", 1)}, "T1", "done", ErrNotFound},
		{"events of another topic are not seen", []entity.Event{foreign}, "T1", "done", ErrNotFound},
		{"an event is being delivered", []entity.Event{leased}, "T1", "done", ErrInProgress},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := memrepo.New()
			for _, event := range tt.events {
				repo.Events[event.EventID] = event
			}
			transport := memrepo.NewTransport()
			c := testCore(repo, transport)
			_, err := c.ResolveTask(context.Background(), agent("rior.eventlog"), tt.taskKey, "", tt.text)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("ResolveTask() error = %v, want %v", err, tt.wantErr)
			}
			if len(transport.Messages) != 0 || transport.DeletedCount() != 0 {
				t.Errorf("sent %d, deleted %d, want nothing touched", len(transport.Messages), transport.DeletedCount())
			}
		})
	}
}

func TestResolveTask(t *testing.T) {
	repo := memrepo.New()
	// two alerts of one task reached chats 1 and 2, a third one only chat 1;
	// another task stays as it is
	for _, event := range []entity.Event{
		triaged("e1", "T1", 1, 2),
		triaged("e2", "T1", 1, 2),
		triaged("e3", "T1", 1),
		triaged("e4", "T2", 1),
	} {
		repo.Events[event.EventID] = event
	}
	transport := memrepo.NewTransport()
	c := testCore(repo, transport)

	outcome, err := c.ResolveTask(context.Background(), agent("rior.eventlog"), "T1", "", "fixed in 1C")
	if err != nil {
		t.Fatalf("ResolveTask() error = %v", err)
	}
	if !outcome.IsComplete() {
		t.Errorf("outcome is not complete: %+v", outcome)
	}
	if got := transport.SentTo(); len(got) != 2 || got[0] == got[1] {
		t.Errorf("outcome sent to %v, want once to chat 1 and once to chat 2", got)
	}
	if got := transport.DeletedCount(); got != 5 {
		t.Errorf("deleted %d messages, want 5", got)
	}

	result := repo.Events[outcome.Result.EventID]
	if result.Resolves != "T1" || result.Type != OutcomeType || result.DedupKey != "resolve:T1" || result.Status != entity.EventDelivered {
		t.Errorf("result event = %+v", result)
	}
	if result.Triage == nil || result.Triage.TaskKey != "T1" {
		t.Errorf("result triage = %+v, want the task", result.Triage)
	}
	for _, id := range []string{"e1", "e2", "e3"} {
		stored := repo.Events[id]
		if stored.Resolution == nil || stored.Resolution.ResultID != result.EventID || stored.Resolution.By != "hermes" {
			t.Errorf("%s resolution = %+v", id, stored.Resolution)
		}
		if stored.Retraction == nil || leftInChats(&stored) {
			t.Errorf("%s is not retracted: %+v", id, stored)
		}
	}
	if untouched := repo.Events["e4"]; untouched.Resolution != nil || untouched.Retraction != nil {
		t.Errorf("event of another task was touched: %+v", untouched)
	}

	// the outcome is out of the feed and in the task's own list
	untriaged := false
	feed, err := c.FindEvents(agent("rior.eventlog"), entity.EventFilter{Triaged: &untriaged})
	if err != nil {
		t.Fatal(err)
	}
	if len(feed) != 0 {
		t.Errorf("feed = %d events, want none", len(feed))
	}
	ofTask, err := c.FindEvents(agent("rior.eventlog"), entity.EventFilter{TaskKey: "T1"})
	if err != nil {
		t.Fatal(err)
	}
	if len(ofTask) != 4 {
		t.Errorf("events of the task = %d, want the 3 alerts and the outcome", len(ofTask))
	}
}

func TestResolveTaskRepeat(t *testing.T) {
	repo := memrepo.New()
	repo.Events["e1"] = triaged("e1", "T1", 1, 2)
	transport := memrepo.NewTransport()
	transport.FailFor[2] = errors.New("chat not found")
	c := testCore(repo, transport)
	ctx := context.Background()

	// the outcome does not reach chat 2: nothing is retracted yet
	outcome, err := c.ResolveTask(ctx, agent("rior.eventlog"), "T1", "", "fixed")
	if err != nil {
		t.Fatal(err)
	}
	if outcome.IsComplete() || outcome.Result.Status != entity.EventPartial {
		t.Errorf("first call: complete = %v, result status = %s", outcome.IsComplete(), outcome.Result.Status)
	}
	if transport.DeletedCount() != 0 || repo.Events["e1"].Resolution != nil {
		t.Errorf("event retracted or resolved before the outcome reached every chat")
	}

	// the repeat sends only to chat 2, then retracts
	delete(transport.FailFor, 2)
	outcome, err = c.ResolveTask(ctx, agent("rior.eventlog"), "T1", "", "fixed")
	if err != nil {
		t.Fatal(err)
	}
	if !outcome.IsComplete() {
		t.Errorf("second call is not complete: %+v", outcome)
	}
	if got := transport.SentTo(); len(got) != 2 || got[1] != 2 {
		t.Errorf("sent to %v, want chat 1 on the first call and chat 2 on the second", got)
	}
	resolvedAt := repo.Events["e1"].Resolution.At

	// a third call changes nothing
	if _, err = c.ResolveTask(ctx, agent("rior.eventlog"), "T1", "", "fixed again"); err != nil {
		t.Fatal(err)
	}
	if len(transport.Messages) != 2 || transport.DeletedCount() != 2 {
		t.Errorf("third call sent %d, deleted %d, want 2 and 2 in total", len(transport.Messages), transport.DeletedCount())
	}
	if !repo.Events["e1"].Resolution.At.Equal(resolvedAt) {
		t.Errorf("resolution was rewritten by the repeat")
	}
}

func TestResolveTaskFinishesRefusedRetraction(t *testing.T) {
	repo := memrepo.New()
	repo.Events["e1"] = triaged("e1", "T1", 1, 2)
	transport := memrepo.NewTransport()
	transport.DeleteFailFor[2] = errors.New("message can't be deleted")
	c := testCore(repo, transport)
	ctx := context.Background()

	outcome, err := c.ResolveTask(ctx, agent("rior.eventlog"), "T1", "", "fixed")
	if err != nil {
		t.Fatal(err)
	}
	if outcome.IsComplete() {
		t.Errorf("complete while Telegram refused a deletion")
	}
	if repo.Events["e1"].Resolution == nil {
		t.Errorf("event is not marked resolved")
	}

	delete(transport.DeleteFailFor, 2)
	outcome, err = c.ResolveTask(ctx, agent("rior.eventlog"), "T1", "", "fixed")
	if err != nil {
		t.Fatal(err)
	}
	if !outcome.IsComplete() || len(transport.Messages) != 2 || transport.DeletedCount() != 2 {
		t.Errorf("repeat: complete = %v, sent %d, deleted %d", outcome.IsComplete(), len(transport.Messages), transport.DeletedCount())
	}
}

func TestResolveTaskAfterManualRetract(t *testing.T) {
	repo := memrepo.New()
	event := triaged("e1", "T1", 1)
	event.Deliveries[0].Status = entity.DeliveryRetracted
	event.Retraction = &entity.Retraction{Reason: "duplicate", By: "hermes"}
	repo.Events["e1"] = event
	transport := memrepo.NewTransport()
	c := testCore(repo, transport)

	outcome, err := c.ResolveTask(context.Background(), agent("rior.eventlog"), "T1", "", "fixed")
	if err != nil {
		t.Fatal(err)
	}
	if !outcome.IsComplete() {
		t.Errorf("outcome is not complete: %+v", outcome)
	}
	if got := transport.SentTo(); len(got) != 1 || got[0] != 1 {
		t.Errorf("outcome sent to %v, want the chat the alert reached", got)
	}
	stored := repo.Events["e1"]
	if transport.DeletedCount() != 0 || stored.Retraction.Reason != "duplicate" || stored.Resolution == nil {
		t.Errorf("stored = %+v, deleted %d", stored, transport.DeletedCount())
	}
}
