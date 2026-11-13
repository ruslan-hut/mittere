package entity

import (
	"mittere/internal/lib/validate"
	"net/http"
	"time"
)

// Sender scopes: send may only publish messages, admin may do everything.
const (
	ScopeSend  = "send"
	ScopeAdmin = "admin"
)

// Sender is an API client: a token plus the topics it may publish to.
type Sender struct {
	Username      string    `json:"username" bson:"username" validate:"required"`
	Name          string    `json:"name,omitempty" bson:"name"`
	Token         string    `json:"token,omitempty" bson:"token"`
	Scope         string    `json:"scope,omitempty" bson:"scope"`
	AllowedTopics []string  `json:"allowed_topics,omitempty" bson:"allowed_topics"`
	State         string    `json:"state,omitempty" bson:"state"`
	CreatedAt     time.Time `json:"created_at,omitempty" bson:"created_at"`
}

func NewSender(username, scope string) Sender {
	return Sender{
		Username:  username,
		Scope:     scope,
		State:     StateActive,
		CreatedAt: time.Now().UTC(),
	}
}

func (s *Sender) Bind(_ *http.Request) error {
	if s.Scope == "" {
		s.Scope = ScopeSend
	}
	if s.State == "" {
		s.State = StateActive
	}
	if s.CreatedAt.IsZero() {
		s.CreatedAt = time.Now().UTC()
	}
	return validate.Struct(s)
}

func (s *Sender) IsAdmin() bool {
	return s.Scope == ScopeAdmin
}

func (s *Sender) IsActive() bool {
	return s.State == StateActive
}

// MayPublish reports whether the sender may publish to a topic. An admin, or a
// sender with no explicit list, may publish to any topic.
func (s *Sender) MayPublish(topic string) bool {
	if !s.IsActive() {
		return false
	}
	if s.IsAdmin() || len(s.AllowedTopics) == 0 {
		return true
	}
	for _, t := range s.AllowedTopics {
		if t == topic {
			return true
		}
	}
	return false
}
