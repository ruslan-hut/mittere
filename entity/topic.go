package entity

import (
	"fmt"
	"mittere/internal/lib/validate"
	"net/http"
	"regexp"
	"time"
)

// TopicDefault receives messages sent without a topic, including those from the
// legacy POST /tg/msg endpoint.
const TopicDefault = "default"

// TopicAdmin receives legacy messages addressed to the admin role.
const TopicAdmin = "admin"

var topicCode = regexp.MustCompile(`^[a-z0-9]+([._-][a-z0-9]+)*$`)

// Topic is a named stream of notifications, such as marketing.shipments.
type Topic struct {
	Code        string    `json:"code" bson:"code" validate:"required"`
	Title       string    `json:"title,omitempty" bson:"title"`
	Description string    `json:"description,omitempty" bson:"description"`
	Bot         string    `json:"bot,omitempty" bson:"bot"`
	CreatedAt   time.Time `json:"created_at,omitempty" bson:"created_at"`
}

func NewTopic(code, title string) Topic {
	return Topic{Code: code, Title: title, CreatedAt: time.Now().UTC()}
}

func (t *Topic) Bind(_ *http.Request) error {
	if err := validate.Struct(t); err != nil {
		return err
	}
	return ValidateTopicCode(t.Code)
}

// ValidateTopicCode checks a topic code: lowercase words joined by . _ or -
func ValidateTopicCode(code string) error {
	if code == "" {
		return fmt.Errorf("topic code is empty")
	}
	if len(code) > 64 {
		return fmt.Errorf("topic code is too long")
	}
	if !topicCode.MatchString(code) {
		return fmt.Errorf("topic code must be lowercase words separated by . _ or -")
	}
	return nil
}
