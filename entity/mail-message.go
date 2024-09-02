package entity

import (
	"mittere/internal/lib/validate"
	"net/http"
)

type MailMessage struct {
	To      string `json:"to" validate:"required,email"`
	Message string `json:"message" validate:"omitempty"`
}

func (m *MailMessage) Bind(_ *http.Request) error {
	return validate.Struct(m)
}
