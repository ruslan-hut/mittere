package entity

import (
	"mittere/internal/lib/validate"
	"net/http"
	"time"
)

type EventMessage struct {
	Sender   *User       `json:"sender,omitempty" bson:"sender"`
	Type     string      `json:"type" bson:"type" validate:"required,min=1"`
	Time     time.Time   `json:"time,omitempty" bson:"time"`
	Username string      `json:"username,omitempty" bson:"username"`
	Text     string      `json:"text,omitempty" bson:"text"`
	Payload  interface{} `json:"payload,omitempty" bson:"payload"`
}

func (m *EventMessage) Bind(_ *http.Request) error {
	return validate.Struct(m)
}
