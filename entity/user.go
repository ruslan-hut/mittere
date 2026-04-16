package entity

import (
	"mittere/internal/lib/validate"
	"net/http"
)

type User struct {
	Username string `json:"username" bson:"username" validate:"required"`
	Name     string `json:"name,omitempty" bson:"name" validate:"omitempty"`
	Email    string `json:"email,omitempty" bson:"email" validate:"omitempty"`
	Token    string `json:"token,omitempty" bson:"token" validate:"required,min=1"`
}

func (u *User) Bind(_ *http.Request) error {
	return validate.Struct(u)
}
