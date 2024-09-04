package entity

import (
	"mittere/internal/lib/validate"
	"net/http"
)

type MailMessage struct {
	Sender  *User  `json:"sender,omitempty" bson:"sender"`
	To      string `json:"to" validate:"required,email"`
	Message string `json:"message" validate:"omitempty"`
}

func (m *MailMessage) Bind(_ *http.Request) error {
	return validate.Struct(m)
}
