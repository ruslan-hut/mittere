package api

import (
	"encoding/json"
	"mittere/entity"
	"mittere/internal/lib/memrepo"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

const agentToken = "agent-token"

// agentRepo holds an agent reading rior.eventlog and one event in that topic
// and one in another.
func agentRepo() *memrepo.Repo {
	repo := memrepo.New().WithTopic("rior.eventlog", 111).WithTopic("ops", 222)
	repo.Senders["hermes"] = entity.Sender{
		Username: "hermes", Token: agentToken, Scope: entity.ScopeAgent, State: entity.StateActive,
		AllowedTopics: []string{"rior.eventlog"},
	}
	repo.Senders["1c"] = entity.Sender{
		Username: "1c", Token: "send-only", Scope: entity.ScopeSend, State: entity.StateActive,
	}
	now := time.Now().UTC()
	repo.Events["own"] = entity.Event{EventID: "own", Topic: "rior.eventlog", Status: entity.EventDelivered, CreatedAt: now,
		Deliveries: []entity.Delivery{{ChannelID: "111", ChatID: 111, Status: entity.DeliverySent, MessageID: 7}}}
	repo.Events["foreign"] = entity.Event{EventID: "foreign", Topic: "ops", Status: entity.EventDelivered, CreatedAt: now,
		Deliveries: []entity.Delivery{{ChannelID: "222", ChatID: 222, Status: entity.DeliverySent, MessageID: 8}}}
	return repo
}

func call(t *testing.T, router http.Handler, method, path, token string, body interface{}) *httptest.ResponseRecorder {
	t.Helper()
	if method == http.MethodPost {
		return post(t, router, path, token, body)
	}
	request := httptest.NewRequest(method, path, nil)
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	return recorder
}

// TestAgentRights checks that an agent reads and works only the journal of its
// topics and reaches nothing else.
func TestAgentRights(t *testing.T) {
	triage := map[string]interface{}{"verdict": "noise"}
	retract := map[string]interface{}{"reason": "duplicate"}
	message := map[string]interface{}{"topic": "rior.eventlog", "type": "alert"}

	tests := []struct {
		name   string
		method string
		path   string
		token  string
		body   interface{}
		want   int
	}{
		{"agent may not publish", http.MethodPost, "/messages", agentToken, message, http.StatusForbidden},
		{"agent may not publish legacy", http.MethodGet, "/tg/test?topic=rior.eventlog", agentToken, nil, http.StatusForbidden},
		{"agent may not read topics", http.MethodGet, "/topics", agentToken, nil, http.StatusForbidden},
		{"agent may not read channels", http.MethodGet, "/channels", agentToken, nil, http.StatusForbidden},
		{"agent may not read senders", http.MethodGet, "/senders", agentToken, nil, http.StatusForbidden},
		{"agent reads its journal", http.MethodGet, "/events", agentToken, nil, http.StatusOK},
		{"agent reads its topic", http.MethodGet, "/events?topic=rior.eventlog", agentToken, nil, http.StatusOK},
		{"agent may not read another topic", http.MethodGet, "/events?topic=ops", agentToken, nil, http.StatusForbidden},
		{"agent reads its event", http.MethodGet, "/events/own", agentToken, nil, http.StatusOK},
		{"agent may not read another event", http.MethodGet, "/events/foreign", agentToken, nil, http.StatusForbidden},
		{"agent may not page from another event", http.MethodGet, "/events?after=foreign", agentToken, nil, http.StatusForbidden},
		{"unknown cursor", http.MethodGet, "/events?after=nothing", agentToken, nil, http.StatusBadRequest},
		{"bad triaged", http.MethodGet, "/events?triaged=maybe", agentToken, nil, http.StatusBadRequest},
		{"agent triages its event", http.MethodPost, "/events/own/triage", agentToken, triage, http.StatusOK},
		{"agent may not triage another event", http.MethodPost, "/events/foreign/triage", agentToken, triage, http.StatusForbidden},
		{"triage of a missing event", http.MethodPost, "/events/nothing/triage", agentToken, triage, http.StatusNotFound},
		{"unknown verdict", http.MethodPost, "/events/own/triage", agentToken, map[string]interface{}{"verdict": "later"}, http.StatusBadRequest},
		{"agent may not retract another event", http.MethodPost, "/events/foreign/retract", agentToken, retract, http.StatusForbidden},
		{"retract without a reason", http.MethodPost, "/events/own/retract", agentToken, map[string]interface{}{}, http.StatusBadRequest},
		{"agent retracts its event", http.MethodPost, "/events/own/retract", agentToken, retract, http.StatusOK},
		{"send token may not triage", http.MethodPost, "/events/own/triage", "send-only", triage, http.StatusForbidden},
		{"admin triages any event", http.MethodPost, "/events/foreign/triage", adminToken, triage, http.StatusOK},
	}

	router := testServer(agentRepo(), memrepo.NewTransport())
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorder := call(t, router, tt.method, tt.path, tt.token, tt.body)
			if recorder.Code != tt.want {
				t.Errorf("status = %d, want %d (%s)", recorder.Code, tt.want, recorder.Body.String())
			}
		})
	}
}

// TestAgentWithoutTopicsReadsNothing checks that an empty list gives an agent
// no topic at all, unlike a send token, for which it means any topic.
func TestAgentWithoutTopicsReadsNothing(t *testing.T) {
	repo := agentRepo()
	repo.Senders["hermes"] = entity.Sender{Username: "hermes", Token: agentToken, Scope: entity.ScopeAgent, State: entity.StateActive}
	recorder := call(t, testServer(repo, memrepo.NewTransport()), http.MethodGet, "/events", agentToken, nil)
	if recorder.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d", recorder.Code, http.StatusForbidden)
	}
}

// TestEventFeed walks the feed the way the agent reads it: what is not yet
// triaged, oldest first, then on from the last event seen.
func TestEventFeed(t *testing.T) {
	repo := agentRepo()
	base := time.Now().UTC().Add(-time.Hour)
	for i, id := range []string{"e1", "e2", "e3", "e4"} {
		repo.Events[id] = entity.Event{EventID: id, Topic: "rior.eventlog", Status: entity.EventDelivered,
			CreatedAt: base.Add(time.Duration(i) * time.Minute)}
	}
	// two events in the same instant are told apart by their identifier
	repo.Events["e3b"] = entity.Event{EventID: "e3b", Topic: "rior.eventlog", Status: entity.EventDelivered,
		CreatedAt: base.Add(2 * time.Minute)}
	repo.Events["e2"] = withTriage(repo.Events["e2"])
	delete(repo.Events, "own")
	router := testServer(repo, memrepo.NewTransport())

	tests := []struct {
		name string
		path string
		want []string
	}{
		{"untriaged, oldest first", "/events?triaged=false", []string{"e1", "e3", "e3b", "e4"}},
		{"triaged only", "/events?triaged=true", []string{"e2"}},
		{"after a cursor", "/events?after=e1", []string{"e2", "e3", "e3b", "e4"}},
		{"after a cursor, untriaged", "/events?triaged=false&after=e1", []string{"e3", "e3b", "e4"}},
		{"cursor in a shared instant", "/events?after=e3", []string{"e3b", "e4"}},
		{"page size", "/events?triaged=false&limit=2", []string{"e1", "e3"}},
		{"nothing after the last", "/events?after=e4", []string{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorder := call(t, router, http.MethodGet, tt.path, agentToken, nil)
			if recorder.Code != http.StatusOK {
				t.Fatalf("status = %d (%s)", recorder.Code, recorder.Body.String())
			}
			var body struct {
				Data []entity.Event `json:"data"`
			}
			if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode: %v", err)
			}
			got := make([]string, 0, len(body.Data))
			for _, event := range body.Data {
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
}

func withTriage(event entity.Event) entity.Event {
	event.Triage = &entity.Triage{Verdict: entity.VerdictNoise, By: "hermes", At: time.Now().UTC()}
	return event
}

// TestRetractAnswer checks the status of a retraction: 207 while Telegram
// refused to delete some message, 200 once nothing is left in any chat.
func TestRetractAnswer(t *testing.T) {
	repo := agentRepo()
	repo.Events["two"] = entity.Event{EventID: "two", Topic: "rior.eventlog", Status: entity.EventDelivered,
		CreatedAt: time.Now().UTC(), Deliveries: []entity.Delivery{
			{ChannelID: "111", ChatID: 111, Status: entity.DeliverySent, MessageID: 1},
			{ChannelID: "333", ChatID: 333, Status: entity.DeliverySent, MessageID: 2},
		}}
	transport := memrepo.NewTransport()
	transport.DeleteFailFor[333] = errString("Bad Request: message can't be deleted")
	router := testServer(repo, transport)

	recorder := post(t, router, "/events/two/retract", agentToken, map[string]interface{}{"reason": "noise"})
	if recorder.Code != http.StatusMultiStatus {
		t.Fatalf("status = %d, want %d (%s)", recorder.Code, http.StatusMultiStatus, recorder.Body.String())
	}
	delete(transport.DeleteFailFor, 333)
	recorder = post(t, router, "/events/two/retract", agentToken, map[string]interface{}{"reason": "noise"})
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (%s)", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if transport.DeletedCount() != 2 {
		t.Errorf("deleted %d messages, want each one once", transport.DeletedCount())
	}
}

type errString string

func (e errString) Error() string { return string(e) }
