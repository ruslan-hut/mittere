package entity

import (
	"mittere/internal/lib/validate"
	"net/http"
	"time"
)

type EventMessage struct {
	Type     string      `json:"type,omitempty" bson:"type"`
	Time     time.Time   `json:"time,omitempty" bson:"time"`
	Username string      `json:"username,omitempty" bson:"username"`
	Status   string      `json:"status,omitempty" bson:"status"`
	Info     string      `json:"info,omitempty" bson:"info"`
	Payload  interface{} `json:"payload,omitempty" bson:"payload"`
}

func (m *EventMessage) Bind(_ *http.Request) error {
	return validate.Struct(m)
}
