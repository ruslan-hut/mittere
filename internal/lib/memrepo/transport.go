package memrepo

import (
	"context"
	"fmt"
	"mittere/entity"
	"sync"
)

// Sent is one call of the transport.
type Sent struct {
	ChatID   int64
	ThreadID int
	Text     string
}

// Deleted is one message the transport was asked to delete.
type Deleted struct {
	ChatID    int64
	MessageID int
}

// Transport is a message transport that records what it was asked to send and
// to delete, and fails for the chats listed in FailFor and DeleteFailFor.
type Transport struct {
	mu            sync.Mutex
	Messages      []Sent
	Deletions     []Deleted
	FailFor       map[int64]error
	DeleteFailFor map[int64]error
	Attempts      int // attempts reported per call, 1 when zero
	Down          bool
}

func NewTransport() *Transport {
	return &Transport{FailFor: make(map[int64]error), DeleteFailFor: make(map[int64]error)}
}

func (t *Transport) Format(msg *entity.Message) string {
	return fmt.Sprintf("%s: %s", msg.Type, msg.Text)
}

func (t *Transport) Send(_ context.Context, chatID int64, threadID int, text string) (int, int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	attempts := t.Attempts
	if attempts == 0 {
		attempts = 1
	}
	if err, ok := t.FailFor[chatID]; ok {
		return 0, attempts, err
	}
	t.Messages = append(t.Messages, Sent{ChatID: chatID, ThreadID: threadID, Text: text})
	return len(t.Messages), attempts, nil
}

func (t *Transport) Delete(_ context.Context, chatID int64, messageID int) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if err, ok := t.DeleteFailFor[chatID]; ok {
		return err
	}
	t.Deletions = append(t.Deletions, Deleted{ChatID: chatID, MessageID: messageID})
	return nil
}

// DeletedCount returns how many messages the transport deleted.
func (t *Transport) DeletedCount() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.Deletions)
}

func (t *Transport) BotName() string { return "test_bot" }

func (t *Transport) Alive() bool { return !t.Down }

// SentTo returns the chats the transport was asked to send to.
func (t *Transport) SentTo() []int64 {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]int64, 0, len(t.Messages))
	for _, message := range t.Messages {
		out = append(out, message.ChatID)
	}
	return out
}
