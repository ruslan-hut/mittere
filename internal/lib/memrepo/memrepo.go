// Package memrepo holds an in-memory implementation of the core repository and
// of the message transport. It exists for the tests: the routing and the
// delivery accounting can be checked without MongoDB and without Telegram.
package memrepo

import (
	"context"
	"fmt"
	"mittere/entity"
	"sort"
	"sync"
	"time"
)

// Repo is an in-memory repository.
type Repo struct {
	mu        sync.Mutex
	Channels  map[string]entity.Channel
	Topics    map[string]entity.Topic
	Subs      map[string]map[string]bool // topic -> channel ids
	Senders   map[string]entity.Sender
	Events    map[string]entity.Event
	LinkCodes map[string]entity.LinkCode
	Fail      error // when set, every read fails with it
}

func New() *Repo {
	return &Repo{
		Channels:  make(map[string]entity.Channel),
		Topics:    make(map[string]entity.Topic),
		Subs:      make(map[string]map[string]bool),
		Senders:   make(map[string]entity.Sender),
		Events:    make(map[string]entity.Event),
		LinkCodes: make(map[string]entity.LinkCode),
	}
}

// WithTopic adds a topic and the active channels subscribed to it.
func (r *Repo) WithTopic(code string, chatIDs ...int64) *Repo {
	r.Topics[code] = entity.NewTopic(code, "")
	for _, chatID := range chatIDs {
		channel := entity.NewChannel(chatID, 0, entity.ChannelKind(chatID, 0))
		r.Channels[channel.ChannelID] = channel
		if r.Subs[code] == nil {
			r.Subs[code] = make(map[string]bool)
		}
		r.Subs[code][channel.ChannelID] = true
	}
	return r
}

func (r *Repo) Ping(context.Context) error { return r.Fail }

func (r *Repo) GetChannels() ([]entity.Channel, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]entity.Channel, 0, len(r.Channels))
	for _, channel := range r.Channels {
		out = append(out, channel)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ChannelID < out[j].ChannelID })
	return out, r.Fail
}

func (r *Repo) GetChannel(channelID string) (*entity.Channel, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.Fail != nil {
		return nil, r.Fail
	}
	channel, ok := r.Channels[channelID]
	if !ok {
		return nil, nil
	}
	return &channel, nil
}

func (r *Repo) SaveChannel(channel *entity.Channel) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.Channels[channel.ChannelID] = *channel
	return r.Fail
}

func (r *Repo) DeleteChannel(channelID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.Channels[channelID]; !ok {
		return fmt.Errorf("channel not found")
	}
	delete(r.Channels, channelID)
	for _, channels := range r.Subs {
		delete(channels, channelID)
	}
	return nil
}

func (r *Repo) GetChannelsByTopic(topic string) ([]entity.Channel, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.Fail != nil {
		return nil, r.Fail
	}
	out := make([]entity.Channel, 0)
	for channelID := range r.Subs[topic] {
		channel, ok := r.Channels[channelID]
		if ok && channel.IsActive() {
			out = append(out, channel)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ChannelID < out[j].ChannelID })
	return out, nil
}

func (r *Repo) GetChannelsByUsername(username string) ([]entity.Channel, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.Fail != nil {
		return nil, r.Fail
	}
	out := make([]entity.Channel, 0)
	for _, channel := range r.Channels {
		if channel.Username == username && channel.IsActive() {
			out = append(out, channel)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ChannelID < out[j].ChannelID })
	return out, nil
}

func (r *Repo) CountChannels(context.Context) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return int64(len(r.Channels)), nil
}

func (r *Repo) GetTopics() ([]entity.Topic, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]entity.Topic, 0, len(r.Topics))
	for _, topic := range r.Topics {
		out = append(out, topic)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Code < out[j].Code })
	return out, r.Fail
}

func (r *Repo) GetTopic(code string) (*entity.Topic, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.Fail != nil {
		return nil, r.Fail
	}
	topic, ok := r.Topics[code]
	if !ok {
		return nil, nil
	}
	return &topic, nil
}

func (r *Repo) CreateTopic(topic *entity.Topic) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.Topics[topic.Code]; ok {
		return fmt.Errorf("topic exists")
	}
	r.Topics[topic.Code] = *topic
	return nil
}

func (r *Repo) EnsureTopic(topic *entity.Topic) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.Topics[topic.Code]; !ok {
		r.Topics[topic.Code] = *topic
	}
	return nil
}

func (r *Repo) UpdateTopic(topic *entity.Topic) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.Topics[topic.Code]; !ok {
		return fmt.Errorf("topic not found")
	}
	r.Topics[topic.Code] = *topic
	return nil
}

func (r *Repo) DeleteTopic(code string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.Topics[code]; !ok {
		return fmt.Errorf("topic not found")
	}
	delete(r.Topics, code)
	delete(r.Subs, code)
	return nil
}

func (r *Repo) Subscribe(sub *entity.Subscription) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.Subs[sub.TopicCode] == nil {
		r.Subs[sub.TopicCode] = make(map[string]bool)
	}
	r.Subs[sub.TopicCode][sub.ChannelID] = true
	return nil
}

func (r *Repo) Unsubscribe(topic, channelID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.Subs[topic][channelID] {
		return fmt.Errorf("subscription not found")
	}
	delete(r.Subs[topic], channelID)
	return nil
}

func (r *Repo) GetTopicsOfChannel(channelID string) ([]string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, 0)
	for topic, channels := range r.Subs {
		if channels[channelID] {
			out = append(out, topic)
		}
	}
	sort.Strings(out)
	return out, nil
}

func (r *Repo) SetTopicsOfChannel(channelID string, topics []string, _ string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, channels := range r.Subs {
		delete(channels, channelID)
	}
	for _, topic := range topics {
		if r.Subs[topic] == nil {
			r.Subs[topic] = make(map[string]bool)
		}
		r.Subs[topic][channelID] = true
	}
	return nil
}

func (r *Repo) GetSenderByToken(token string) (*entity.Sender, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.Fail != nil {
		return nil, r.Fail
	}
	for _, sender := range r.Senders {
		if sender.Token == token {
			found := sender
			return &found, nil
		}
	}
	return nil, nil
}

func (r *Repo) GetSenders() ([]entity.Sender, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]entity.Sender, 0, len(r.Senders))
	for _, sender := range r.Senders {
		out = append(out, sender)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Username < out[j].Username })
	return out, nil
}

func (r *Repo) GetSender(username string) (*entity.Sender, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	sender, ok := r.Senders[username]
	if !ok {
		return nil, nil
	}
	return &sender, nil
}

func (r *Repo) CreateSender(sender *entity.Sender) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.Senders[sender.Username]; ok {
		return fmt.Errorf("sender exists")
	}
	r.Senders[sender.Username] = *sender
	return nil
}

func (r *Repo) UpdateSender(sender *entity.Sender) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.Senders[sender.Username]; !ok {
		return fmt.Errorf("sender not found")
	}
	r.Senders[sender.Username] = *sender
	return nil
}

func (r *Repo) DeleteSender(username string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.Senders[username]; !ok {
		return fmt.Errorf("sender not found")
	}
	delete(r.Senders, username)
	return nil
}

// ClaimEvent mirrors the storage: it takes the event for delivery and returns
// what an earlier call with the same dedup key left behind.
func (r *Repo) ClaimEvent(event *entity.Event, lease time.Duration) (*entity.Event, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now().UTC()
	until := now.Add(lease)
	event.LeaseUntil = &until
	if event.DedupKey == "" {
		r.Events[event.EventID] = *event
		return nil, nil
	}
	for _, stored := range r.Events {
		if stored.DedupKey != event.DedupKey {
			continue
		}
		if stored.LeaseUntil != nil && stored.LeaseUntil.After(now) {
			return nil, entity.ErrInProgress
		}
		previous := stored
		stored.LeaseUntil = &until
		r.Events[stored.EventID] = stored
		return &previous, nil
	}
	r.Events[event.EventID] = *event
	return nil, nil
}

func (r *Repo) SaveEvent(event *entity.Event) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.Events[event.EventID] = *event
	return nil
}

func (r *Repo) GetEvent(eventID string) (*entity.Event, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	event, ok := r.Events[eventID]
	if !ok {
		return nil, nil
	}
	return &event, nil
}

func (r *Repo) GetEventByDedupKey(key string) (*entity.Event, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, event := range r.Events {
		if event.DedupKey == key {
			found := event
			return &found, nil
		}
	}
	return nil, nil
}

func (r *Repo) FindEvents(filter entity.EventFilter) ([]entity.Event, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]entity.Event, 0)
	for _, event := range r.Events {
		if filter.Topic != "" && event.Topic != filter.Topic {
			continue
		}
		if filter.Topic == "" && filter.Topics != nil && !contains(filter.Topics, event.Topic) {
			continue
		}
		if filter.TaskKey != "" && (event.Triage == nil || event.Triage.TaskKey != filter.TaskKey) {
			continue
		}
		if filter.Triaged != nil && (event.Triage != nil) != *filter.Triaged {
			continue
		}
		if filter.AfterID != "" && !after(event, filter.AfterTime, filter.AfterID) {
			continue
		}
		if filter.Status != "" && event.Status != filter.Status {
			continue
		}
		if !filter.From.IsZero() && event.CreatedAt.Before(filter.From) {
			continue
		}
		if !filter.To.IsZero() && event.CreatedAt.After(filter.To) {
			continue
		}
		out = append(out, event)
	}
	ascending := filter.Ascending || filter.AfterID != ""
	sort.Slice(out, func(i, j int) bool {
		if ascending {
			return after(out[j], out[i].CreatedAt, out[i].EventID)
		}
		return after(out[i], out[j].CreatedAt, out[j].EventID)
	})
	// the same page size as the storage
	limit := filter.Limit
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	if int64(len(out)) > limit {
		out = out[:limit]
	}
	return out, nil
}

// after reports whether an event comes after a position in the journal.
func after(event entity.Event, at time.Time, id string) bool {
	if event.CreatedAt.Equal(at) {
		return event.EventID > id
	}
	return event.CreatedAt.After(at)
}

func contains(list []string, value string) bool {
	for _, item := range list {
		if item == value {
			return true
		}
	}
	return false
}

func (r *Repo) SetEventTriage(eventID string, triage *entity.Triage) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	event, ok := r.Events[eventID]
	if !ok {
		return entity.ErrNotFound
	}
	if event.IsLeased(time.Now().UTC()) {
		return entity.ErrInProgress
	}
	event.Triage = triage
	r.Events[eventID] = event
	return nil
}

func (r *Repo) SetEventRetraction(eventID string, retraction *entity.Retraction, deliveries []entity.Delivery) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	event, ok := r.Events[eventID]
	if !ok {
		return entity.ErrNotFound
	}
	event.Retraction = retraction
	event.Deliveries = append([]entity.Delivery(nil), deliveries...)
	event.LeaseUntil = nil
	r.Events[eventID] = event
	return nil
}

func (r *Repo) LeaseEvent(eventID string, lease time.Duration) (*entity.Event, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	event, ok := r.Events[eventID]
	if !ok {
		return nil, entity.ErrNotFound
	}
	now := time.Now().UTC()
	if event.IsLeased(now) {
		return nil, entity.ErrInProgress
	}
	until := now.Add(lease)
	event.LeaseUntil = &until
	r.Events[eventID] = event
	event.Deliveries = append([]entity.Delivery(nil), event.Deliveries...)
	return &event, nil
}

func (r *Repo) SetEventResolution(eventID string, resolution *entity.Resolution) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	event, ok := r.Events[eventID]
	if !ok {
		return entity.ErrNotFound
	}
	if event.IsLeased(time.Now().UTC()) {
		return entity.ErrInProgress
	}
	event.Resolution = resolution
	r.Events[eventID] = event
	return nil
}

func (r *Repo) ReleaseEvent(eventID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if event, ok := r.Events[eventID]; ok {
		event.LeaseUntil = nil
		r.Events[eventID] = event
	}
	return nil
}

func (r *Repo) CreateLinkCode(code *entity.LinkCode) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.LinkCodes[code.Code] = *code
	return nil
}

func (r *Repo) GetLinkCode(code string) (*entity.LinkCode, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	stored, ok := r.LinkCodes[code]
	if !ok {
		return nil, nil
	}
	return &stored, nil
}

func (r *Repo) UseLinkCode(code, channelID string, usedAt time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	stored, ok := r.LinkCodes[code]
	if !ok || !stored.IsUsable(usedAt) {
		return fmt.Errorf("code is unknown, expired or already used")
	}
	stored.UsedAt = &usedAt
	stored.ChannelID = channelID
	r.LinkCodes[code] = stored
	return nil
}
