package entity

import (
	"mittere/internal/lib/validate"
	"net/http"
	"time"
)

// EventMessage is the body of the legacy POST /tg/msg. It is kept so the
// notifications 1C sends today keep working until they move to POST /messages.
type EventMessage struct {
	Topic    string      `json:"topic,omitempty"`
	Type     string      `json:"type" validate:"required,min=1"`
	Subject  string      `json:"subject" validate:"required,min=1"`
	Role     string      `json:"role,omitempty"`
	Time     time.Time   `json:"time,omitempty"`
	Username string      `json:"username,omitempty"`
	Text     string      `json:"text,omitempty"`
	Payload  interface{} `json:"payload,omitempty"`
	DedupKey string      `json:"dedup_key,omitempty"`
}

func (m *EventMessage) Bind(_ *http.Request) error {
	return validate.Struct(m)
}

// ToMessage converts a legacy message. Without a topic it goes to the default
// one, or to the admin topic when it was addressed to the admin role.
func (m *EventMessage) ToMessage() *Message {
	return &Message{
		Topic:    m.Topic,
		Type:     m.Type,
		Subject:  m.Subject,
		Text:     m.Text,
		Payload:  m.Payload,
		DedupKey: m.DedupKey,
		Role:     m.Role,
		Username: m.Username,
	}
}
