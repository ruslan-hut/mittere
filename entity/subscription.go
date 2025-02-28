package entity

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
		Role:             "guest",
		State:            "await",
		SubscriptionType: "status",
	}
}

func (s *Subscription) Confirm() {
	s.State = "active"
	s.IsVerified = true
}

func (s *Subscription) Disable() {
	s.State = "disabled"
}

func (s *Subscription) IsAdmin() bool {
	return s.Role == "admin"
}

func (s *Subscription) IsActive() bool {
	return s.State == "active"
}
