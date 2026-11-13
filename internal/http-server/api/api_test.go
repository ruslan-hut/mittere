package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"mittere/entity"
	"mittere/impl/core"
	"mittere/internal/lib/memrepo"
	"net/http"
	"net/http/httptest"
	"testing"
)

const adminToken = "admin-token"

func testServer(repo *memrepo.Repo, transport *memrepo.Transport) http.Handler {
	lg := slog.New(slog.NewTextHandler(io.Discard, nil))
	handler := core.New(repo, lg)
	handler.SetToken(adminToken)
	handler.SetTransport(transport)
	return Router(lg, handler)
}

func post(t *testing.T, router http.Handler, path, token string, body interface{}) *httptest.ResponseRecorder {
	t.Helper()
	payload, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	request := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(payload))
	request.Header.Set("Content-Type", "application/json")
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	return recorder
}

// TestPublishStatusCodes is the contract 1C relies on: it marks a notification
// delivered on 200, so anything short of a full delivery must not answer 200.
func TestPublishStatusCodes(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		body     map[string]interface{}
		failFor  []int64
		channels []int64
		want     int
	}{
		{
			name:     "everyone got it",
			path:     "/messages",
			body:     map[string]interface{}{"topic": "ops", "type": "alert", "text": "hello"},
			channels: []int64{111, 222},
			want:     http.StatusOK,
		},
		{
			name:     "one recipient failed",
			path:     "/messages",
			body:     map[string]interface{}{"topic": "ops", "type": "alert"},
			channels: []int64{111, 222},
			failFor:  []int64{222},
			want:     http.StatusMultiStatus,
		},
		{
			name:     "nobody got it",
			path:     "/messages",
			body:     map[string]interface{}{"topic": "ops", "type": "alert"},
			channels: []int64{111},
			failFor:  []int64{111},
			want:     http.StatusBadGateway,
		},
		{
			name:     "topic has no channels",
			path:     "/messages",
			body:     map[string]interface{}{"topic": "ops", "type": "alert"},
			channels: []int64{},
			want:     http.StatusConflict,
		},
		{
			name:     "unknown topic",
			path:     "/messages",
			body:     map[string]interface{}{"topic": "nowhere", "type": "alert"},
			channels: []int64{111},
			want:     http.StatusNotFound,
		},
		{
			name:     "legacy endpoint without a topic reaches the default one",
			path:     "/tg/msg",
			body:     map[string]interface{}{"type": "alert", "subject": "deployment", "text": "hello"},
			channels: []int64{111},
			want:     http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := memrepo.New()
			topic := "ops"
			if tt.path == "/tg/msg" {
				topic = entity.TopicDefault
			}
			repo.WithTopic(topic, tt.channels...)
			transport := memrepo.NewTransport()
			for _, chatID := range tt.failFor {
				transport.FailFor[chatID] = errors.New("chat not found")
			}

			recorder := post(t, testServer(repo, transport), tt.path, adminToken, tt.body)
			if recorder.Code != tt.want {
				t.Errorf("status = %d, want %d (body %s)", recorder.Code, tt.want, recorder.Body.String())
			}
		})
	}
}

// TestPublishAnswerCarriesDeliveries checks that the answer names every
// recipient and what happened to it.
func TestPublishAnswerCarriesDeliveries(t *testing.T) {
	repo := memrepo.New().WithTopic("ops", 111, 222)
	transport := memrepo.NewTransport()
	transport.FailFor[222] = errors.New("bot is not a member of the chat")

	recorder := post(t, testServer(repo, transport), "/messages", adminToken,
		map[string]interface{}{"topic": "ops", "type": "alert", "text": "hello"})

	var answer struct {
		Data    entity.Event `json:"data"`
		Success bool         `json:"success"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &answer); err != nil {
		t.Fatalf("unmarshal: %v (%s)", err, recorder.Body.String())
	}
	if answer.Success {
		t.Error("success = true for a partial delivery")
	}
	if answer.Data.EventID == "" {
		t.Error("answer carries no event id")
	}
	if len(answer.Data.Deliveries) != 2 {
		t.Fatalf("deliveries = %d, want 2", len(answer.Data.Deliveries))
	}
	for _, delivery := range answer.Data.Deliveries {
		if delivery.ChatID == 222 && delivery.Error == "" {
			t.Error("the failed delivery does not say why")
		}
	}
}

func TestAuthorization(t *testing.T) {
	repo := memrepo.New().WithTopic("ops", 111)
	sendToken := "send-only"
	repo.Senders["1c"] = entity.Sender{
		Username: "1c", Token: sendToken, Scope: entity.ScopeSend, State: entity.StateActive,
		AllowedTopics: []string{"ops"},
	}
	router := testServer(repo, memrepo.NewTransport())

	tests := []struct {
		name   string
		method string
		path   string
		token  string
		want   int
	}{
		{"no token", http.MethodPost, "/messages", "", http.StatusUnauthorized},
		{"unknown token", http.MethodGet, "/topics", "nonsense", http.StatusUnauthorized},
		{"send token may publish", http.MethodPost, "/messages", sendToken, http.StatusOK},
		{"send token may not read topics", http.MethodGet, "/topics", sendToken, http.StatusForbidden},
		{"send token may not read channels", http.MethodGet, "/channels", sendToken, http.StatusForbidden},
		{"send token may not read the journal", http.MethodGet, "/events", sendToken, http.StatusForbidden},
		{"admin token may read topics", http.MethodGet, "/topics", adminToken, http.StatusOK},
		{"health needs no token", http.MethodGet, "/health", "", http.StatusOK},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.method == http.MethodPost {
				recorder := post(t, router, tt.path, tt.token,
					map[string]interface{}{"topic": "ops", "type": "alert"})
				if recorder.Code != tt.want {
					t.Errorf("status = %d, want %d (%s)", recorder.Code, tt.want, recorder.Body.String())
				}
				return
			}
			request := httptest.NewRequest(tt.method, tt.path, nil)
			if tt.token != "" {
				request.Header.Set("Authorization", "Bearer "+tt.token)
			}
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, request)
			if recorder.Code != tt.want {
				t.Errorf("status = %d, want %d (%s)", recorder.Code, tt.want, recorder.Body.String())
			}
		})
	}
}

// TestSendTokenRestrictedToItsTopics checks that a send token cannot publish
// outside the topics it was given.
func TestSendTokenRestrictedToItsTopics(t *testing.T) {
	repo := memrepo.New().WithTopic("ops", 111).WithTopic("marketing.shipments", 222)
	repo.Senders["1c"] = entity.Sender{
		Username: "1c", Token: "send-only", Scope: entity.ScopeSend, State: entity.StateActive,
		AllowedTopics: []string{"marketing.shipments"},
	}
	router := testServer(repo, memrepo.NewTransport())

	recorder := post(t, router, "/messages", "send-only", map[string]interface{}{"topic": "ops", "type": "alert"})
	if recorder.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d", recorder.Code, http.StatusForbidden)
	}
	recorder = post(t, router, "/messages", "send-only", map[string]interface{}{"topic": "marketing.shipments", "type": "alert"})
	if recorder.Code != http.StatusOK {
		t.Errorf("status = %d, want %d (%s)", recorder.Code, http.StatusOK, recorder.Body.String())
	}
}

// TestLinkFlow walks the way a group is attached to a topic: an admin asks for
// a code, the code is sent in the chat, and the chat starts receiving.
func TestLinkFlow(t *testing.T) {
	repo := memrepo.New().WithTopic("marketing.shipments")
	transport := memrepo.NewTransport()
	lg := slog.New(slog.NewTextHandler(io.Discard, nil))
	handler := core.New(repo, lg)
	handler.SetToken(adminToken)
	handler.SetTransport(transport)
	router := Router(lg, handler)

	recorder := post(t, router, "/channels/link", adminToken, map[string]interface{}{"topic": "marketing.shipments"})
	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d (%s)", recorder.Code, http.StatusCreated, recorder.Body.String())
	}
	var created struct {
		Data entity.LinkCode `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &created); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if created.Data.Code == "" {
		t.Fatal("no code issued")
	}

	chat := &entity.ChatInfo{ChatID: -1001234, Kind: entity.ChannelGroup, Title: "Маркетингові відправки", ActorUsername: "ruslan"}
	reply := handler.LinkChat(chat, created.Data.Code)
	if reply == "" {
		t.Fatal("the bot said nothing")
	}

	channel, err := repo.GetChannel(chat.ChannelID())
	if err != nil || channel == nil {
		t.Fatalf("channel not created: %v", err)
	}
	if !channel.IsActive() || channel.Kind != entity.ChannelGroup {
		t.Errorf("channel = %+v, want an active group", channel)
	}

	event, err := handler.Publish(context.Background(), &entity.Message{Topic: "marketing.shipments", Type: "alert"}, nil)
	if err != nil {
		t.Fatalf("Publish() error = %v", err)
	}
	if event.Status != entity.EventDelivered || len(event.Deliveries) != 1 {
		t.Errorf("event = %+v, want one delivered message", event)
	}

	// the same code cannot be spent twice
	if reply = handler.LinkChat(&entity.ChatInfo{ChatID: -999, Kind: entity.ChannelGroup}, created.Data.Code); reply == "" {
		t.Fatal("the bot said nothing")
	}
	if _, err = repo.GetChannel(entity.ChannelKey(-999, 0)); err != nil {
		t.Fatalf("GetChannel() error = %v", err)
	}
	if channel, _ := repo.GetChannel(entity.ChannelKey(-999, 0)); channel != nil {
		t.Error("a used code linked a second chat")
	}
}
