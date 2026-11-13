package entity

import (
	"mittere/internal/lib/validate"
	"net/http"
	"time"
)

// Delivery statuses, per recipient channel.
const (
	DeliveryPending = "pending"
	DeliverySent    = "sent"
	DeliveryFailed  = "failed"
)

// Event statuses, over all recipients of one message.
const (
	EventPending      = "pending"
	EventDelivered    = "delivered"
	EventPartial      = "partial"
	EventFailed       = "failed"
	EventNoRecipients = "no_recipients"
)

// Message is an incoming publication request.
type Message struct {
	Topic    string      `json:"topic,omitempty" bson:"topic"`
	Type     string      `json:"type" bson:"type" validate:"required,min=1"`
	Subject  string      `json:"subject,omitempty" bson:"subject"`
	Text     string      `json:"text,omitempty" bson:"text"`
	Payload  interface{} `json:"payload,omitempty" bson:"payload"`
	DedupKey string      `json:"dedup_key,omitempty" bson:"dedup_key"`

	// Legacy targeting, filled by the POST /tg/msg handler only.
	Role     string `json:"-" bson:"-"`
	Username string `json:"-" bson:"-"`
}

func (m *Message) Bind(_ *http.Request) error {
	return validate.Struct(m)
}

// Delivery is the outcome of sending one event to one channel.
type Delivery struct {
	ChannelID string     `json:"channel_id" bson:"channel_id"`
	ChatID    int64      `json:"chat_id" bson:"chat_id"`
	ThreadID  int        `json:"thread_id,omitempty" bson:"thread_id"`
	Status    string     `json:"status" bson:"status"`
	Attempts  int        `json:"attempts" bson:"attempts"`
	Error     string     `json:"error,omitempty" bson:"error"`
	MessageID int        `json:"message_id,omitempty" bson:"message_id"`
	SentAt    *time.Time `json:"sent_at,omitempty" bson:"sent_at,omitempty"`
}

func (d *Delivery) IsSent() bool {
	return d.Status == DeliverySent
}

// Event is the journal record of one published message and its deliveries.
type Event struct {
	EventID   string      `json:"event_id" bson:"event_id"`
	Topic     string      `json:"topic" bson:"topic"`
	Type      string      `json:"type,omitempty" bson:"type"`
	Subject   string      `json:"subject,omitempty" bson:"subject"`
	Text      string      `json:"text,omitempty" bson:"text"`
	Payload   interface{} `json:"payload,omitempty" bson:"payload"`
	Sender    string      `json:"sender,omitempty" bson:"sender"`
	DedupKey  string      `json:"dedup_key,omitempty" bson:"dedup_key,omitempty"`
	Status    string      `json:"status" bson:"status"`
	CreatedAt time.Time   `json:"created_at" bson:"created_at"`
	// LeaseUntil holds the claim of the call that is delivering this event;
	// while it is in the future no other call may send the same dedup key.
	LeaseUntil *time.Time `json:"-" bson:"lease_until,omitempty"`
	FinishedAt *time.Time `json:"finished_at,omitempty" bson:"finished_at,omitempty"`
	Deliveries []Delivery `json:"deliveries" bson:"deliveries"`
}

// Recalculate sets the event status from the state of its deliveries.
func (e *Event) Recalculate() {
	if len(e.Deliveries) == 0 {
		e.Status = EventNoRecipients
		return
	}
	sent, failed := 0, 0
	for i := range e.Deliveries {
		switch e.Deliveries[i].Status {
		case DeliverySent:
			sent++
		case DeliveryFailed:
			failed++
		}
	}
	switch {
	case sent == len(e.Deliveries):
		e.Status = EventDelivered
	case sent == 0 && failed == len(e.Deliveries):
		e.Status = EventFailed
	case sent+failed < len(e.Deliveries):
		e.Status = EventPending
	default:
		e.Status = EventPartial
	}
}

// EventFilter selects events for GET /events.
type EventFilter struct {
	Topic  string
	Status string
	From   time.Time
	To     time.Time
	Limit  int64
}
