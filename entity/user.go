package entity

import (
	"mittere/internal/lib/validate"
	"net/http"
)

const (
	RoleAdmin = "admin"
	RoleUser  = "user"

	StateAwait    = "await"
	StateActive   = "active"
	StateDisabled = "disabled"
)

type User struct {
	UserID           int64  `json:"user_id,omitempty" bson:"user_id"`
	Username         string `json:"username" bson:"username" validate:"required"`
	Name             string `json:"name,omitempty" bson:"name" validate:"omitempty"`
	Email            string `json:"email,omitempty" bson:"email" validate:"omitempty"`
	Token            string `json:"token,omitempty" bson:"token" validate:"omitempty"`
	Role             string `json:"role,omitempty" bson:"role"`
	State            string `json:"state,omitempty" bson:"state"`
	IsVerified       bool   `json:"is_verified,omitempty" bson:"is_verified"`
	SubscriptionType string `json:"subscription_type,omitempty" bson:"subscription_type"`
}

func NewUser(userId int64, username string) User {
	return User{
		UserID:           userId,
		Username:         username,
		Role:             RoleUser,
		State:            StateAwait,
		SubscriptionType: "status",
	}
}

func (u *User) Bind(_ *http.Request) error {
	return validate.Struct(u)
}

func (u *User) Confirm() {
	u.State = StateActive
	u.IsVerified = true
}

func (u *User) Disable() {
	u.State = StateDisabled
}

func (u *User) IsAdmin() bool {
	return u.Role == RoleAdmin
}

func (u *User) IsActive() bool {
	return u.State == StateActive
}
