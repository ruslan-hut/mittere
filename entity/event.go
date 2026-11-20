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
	// DeliveryRetracted is a sent message deleted from the chat afterwards.
	DeliveryRetracted = "retracted"
)

// Triage verdicts an agent gives an event.
const (
	VerdictTask      = "task"
	VerdictDuplicate = "duplicate"
	VerdictNoise     = "noise"
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

	// Resolves is the task key whose outcome this message reports, set by
	// POST /events/resolve only.
	Resolves string `json:"-" bson:"-"`

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
	LeaseUntil *time.Time  `json:"-" bson:"lease_until,omitempty"`
	FinishedAt *time.Time  `json:"finished_at,omitempty" bson:"finished_at,omitempty"`
	Deliveries []Delivery  `json:"deliveries" bson:"deliveries"`
	Triage     *Triage     `json:"triage,omitempty" bson:"triage,omitempty"`
	Retraction *Retraction `json:"retraction,omitempty" bson:"retraction,omitempty"`
	// Resolution is set on the events a task outcome closed.
	Resolution *Resolution `json:"resolution,omitempty" bson:"resolution,omitempty"`
	// Resolves is set on the outcome message itself: the task key it reports.
	Resolves string `json:"resolves,omitempty" bson:"resolves,omitempty"`
}

// Resolution records that the task an event was triaged to is done: the
// outcome went out as the event ResultID and the event was retracted.
type Resolution struct {
	TaskKey  string    `json:"task_key" bson:"task_key"`
	ResultID string    `json:"result_id" bson:"result_id"`
	By       string    `json:"by" bson:"by"`
	At       time.Time `json:"at" bson:"at"`
}

// TaskOutcome is what POST /events/resolve did: the outcome message and the
// events of the task it closed.
type TaskOutcome struct {
	TaskKey string  `json:"task_key"`
	Result  *Event  `json:"result"`
	Events  []Event `json:"events"`
}

// IsComplete reports whether the outcome reached every chat and every event of
// the task is resolved with nothing of it left in the chats.
func (o *TaskOutcome) IsComplete() bool {
	if o.Result == nil || (o.Result.Status != EventDelivered && o.Result.Status != EventNoRecipients) {
		return false
	}
	for i := range o.Events {
		if o.Events[i].Resolution == nil {
			return false
		}
		for j := range o.Events[i].Deliveries {
			delivery := &o.Events[i].Deliveries[j]
			if delivery.IsSent() && delivery.MessageID != 0 {
				return false
			}
		}
	}
	return true
}

// Triage is the verdict an agent gave an event.
type Triage struct {
	Verdict string    `json:"verdict" bson:"verdict"`
	TaskKey string    `json:"task_key,omitempty" bson:"task_key,omitempty"`
	Note    string    `json:"note,omitempty" bson:"note,omitempty"`
	By      string    `json:"by" bson:"by"`
	At      time.Time `json:"at" bson:"at"`
}

// ValidVerdict reports whether a triage verdict is one the service knows.
func ValidVerdict(verdict string) bool {
	switch verdict {
	case VerdictTask, VerdictDuplicate, VerdictNoise:
		return true
	}
	return false
}

// Retraction records that the messages of an event were deleted from the chats.
type Retraction struct {
	Reason string    `json:"reason" bson:"reason"`
	By     string    `json:"by" bson:"by"`
	At     time.Time `json:"at" bson:"at"`
}

// IsLeased reports whether a call is delivering the event right now.
func (e *Event) IsLeased(now time.Time) bool {
	return e.LeaseUntil != nil && e.LeaseUntil.After(now)
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
	Topics []string // the topics to read when Topic is empty
	Status string
	From   time.Time
	To     time.Time
	Limit  int64
	// TaskKey keeps only the events triaged to this task.
	TaskKey string
	// Triaged, when set, keeps only the events with or without a verdict.
	Triaged *bool
	// After is the event_id of the cursor: only the events created after it,
	// oldest first.
	After string
	// AfterTime and AfterID are the position of the cursor, resolved from After.
	AfterTime time.Time
	AfterID   string
	// Ascending returns the oldest first, as a feed is read.
	Ascending bool
}
