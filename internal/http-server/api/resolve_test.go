package api

import (
	"encoding/json"
	"mittere/entity"
	"mittere/internal/lib/memrepo"
	"net/http"
	"testing"
	"time"
)

// TestResolveAnswer checks the HTTP contract of POST /events/resolve.
func TestResolveAnswer(t *testing.T) {
	resolve := func(key, text string) map[string]interface{} {
		return map[string]interface{}{"task_key": key, "text": text}
	}
	tests := []struct {
		name   string
		token  string
		body   interface{}
		refuse bool
		want   int
	}{
		{"resolved", agentToken, resolve("T1", "fixed"), false, http.StatusOK},
		{"a deletion refused", agentToken, resolve("T1", "fixed"), true, http.StatusMultiStatus},
		{"no text", agentToken, resolve("T1", ""), false, http.StatusBadRequest},
		{"unknown task", agentToken, resolve("T9", "fixed"), false, http.StatusNotFound},
		{"task of another topic", agentToken, resolve("T2", "fixed"), false, http.StatusNotFound},
		{"send token", "send-only", resolve("T1", "fixed"), false, http.StatusForbidden},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := agentRepo()
			own, foreign := repo.Events["own"], repo.Events["foreign"]
			own.Triage = &entity.Triage{Verdict: entity.VerdictTask, TaskKey: "T1", By: "hermes", At: time.Now().UTC()}
			foreign.Triage = &entity.Triage{Verdict: entity.VerdictTask, TaskKey: "T2", By: "admin", At: time.Now().UTC()}
			repo.Events["own"], repo.Events["foreign"] = own, foreign
			transport := memrepo.NewTransport()
			if tt.refuse {
				transport.DeleteFailFor[111] = errString("Bad Request: message can't be deleted")
			}
			router := testServer(repo, transport)

			recorder := post(t, router, "/events/resolve", tt.token, tt.body)
			if recorder.Code != tt.want {
				t.Fatalf("status = %d, want %d (%s)", recorder.Code, tt.want, recorder.Body.String())
			}
			if tt.want != http.StatusOK {
				return
			}
			// the task's list holds the alert and the outcome
			recorder = call(t, router, http.MethodGet, "/events?task_key=T1", agentToken, nil)
			if recorder.Code != http.StatusOK {
				t.Fatalf("GET /events?task_key status = %d", recorder.Code)
			}
			var body struct {
				Data []entity.Event `json:"data"`
			}
			if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if len(body.Data) != 2 {
				t.Errorf("events of the task = %d, want 2", len(body.Data))
			}
		})
	}
}
