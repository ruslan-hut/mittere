package entity

const (
	RoleAdmin = "admin"
	RoleGuest = "guest"

	StateAwait    = "await"
	StateActive   = "active"
	StateDisabled = "disabled"
)

type Subscription struct {
	UserID           int    `json:"user_id" bson:"user_id"`
	User             string `json:"user" bson:"user"`
	Role             string `json:"role" bson:"role"`
	State            string `json:"state" bson:"state"`
	IsVerified       bool   `json:"is_verified" bson:"is_verified"`
	SubscriptionType string `json:"subscription_type" bson:"subscription_type"`
}

func NewSubscription(userId int, user string) Subscription {
	return Subscription{
		UserID:           userId,
		User:             user,
		Role:             RoleGuest,
		State:            StateAwait,
		SubscriptionType: "status",
	}
}

func (s *Subscription) Confirm() {
	s.State = StateActive
	s.IsVerified = true
}

func (s *Subscription) Disable() {
	s.State = StateDisabled
}

func (s *Subscription) IsAdmin() bool {
	return s.Role == RoleAdmin
}

func (s *Subscription) IsActive() bool {
	return s.State == StateActive
}
