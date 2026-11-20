package core

import (
	"context"
	"fmt"
	"log/slog"
	"mittere/entity"
	"mittere/internal/lib/sl"
	"strings"
	"time"
)

// GetEvent returns one journal record. An agent gets only the events of its
// topics.
func (c *Core) GetEvent(sender *entity.Sender, eventID string) (*entity.Event, error) {
	if c.repo == nil {
		return nil, ErrNoRepository
	}
	event, err := c.repo.GetEvent(eventID)
	if err != nil || event == nil {
		return event, err
	}
	if !sender.MayRead(event.Topic) {
		return nil, ErrForbidden
	}
	return event, nil
}

// FindEvents returns journal records matching a filter. An agent is held to
// its topics: asking for another one is refused, asking for none gives all of
// its own. A cursor is resolved here into the position the storage pages from.
func (c *Core) FindEvents(sender *entity.Sender, filter entity.EventFilter) ([]entity.Event, error) {
	if c.repo == nil {
		return nil, ErrNoRepository
	}
	if !sender.IsAdmin() {
		if filter.Topic != "" {
			if !sender.MayRead(filter.Topic) {
				return nil, ErrForbidden
			}
		} else {
			if !sender.IsAgent() || !sender.IsActive() || len(sender.AllowedTopics) == 0 {
				return nil, ErrForbidden
			}
			filter.Topics = sender.AllowedTopics
		}
	}
	if filter.After != "" {
		cursor, err := c.repo.GetEvent(filter.After)
		if err != nil {
			return nil, err
		}
		if cursor == nil {
			return nil, fmt.Errorf("%w: unknown cursor %s", entity.ErrInvalid, filter.After)
		}
		if !sender.MayRead(cursor.Topic) {
			return nil, ErrForbidden
		}
		filter.AfterTime = cursor.CreatedAt
		filter.AfterID = cursor.EventID
		filter.Ascending = true
	}
	return c.repo.FindEvents(filter)
}

// TriageEvent records the verdict an agent gave an event; a repeated verdict
// replaces the earlier one.
func (c *Core) TriageEvent(sender *entity.Sender, eventID string, triage *entity.Triage) (*entity.Event, error) {
	if c.repo == nil {
		return nil, ErrNoRepository
	}
	if !entity.ValidVerdict(triage.Verdict) {
		return nil, fmt.Errorf("%w: unknown verdict %q", entity.ErrInvalid, triage.Verdict)
	}
	event, err := c.eventToWork(sender, eventID)
	if err != nil {
		return nil, err
	}
	triage.By = sender.Username
	triage.At = time.Now().UTC()
	if err = c.repo.SetEventTriage(eventID, triage); err != nil {
		return nil, err
	}
	event.Triage = triage
	return event, nil
}

// RetractEvent deletes the messages of an event from every chat it reached.
// The event is leased for the time it takes, so a retry of the same message
// from 1C waits instead of sending it again or writing over the outcome. A
// delivery Telegram refuses to delete stays sent with the reason in its error,
// so a repeated call tries it again while what is gone is not touched.
func (c *Core) RetractEvent(ctx context.Context, sender *entity.Sender, eventID, reason string) (*entity.Event, error) {
	if c.repo == nil {
		return nil, ErrNoRepository
	}
	if c.transport == nil {
		return nil, ErrNoTransport
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return nil, fmt.Errorf("%w: reason is required", entity.ErrInvalid)
	}
	if _, err := c.eventToWork(sender, eventID); err != nil {
		return nil, err
	}
	event, err := c.repo.LeaseEvent(eventID, c.eventLease)
	if err != nil {
		return nil, err
	}
	log := c.log.With(slog.String("event_id", event.EventID), slog.String("topic", event.Topic))

	for i := range event.Deliveries {
		delivery := &event.Deliveries[i]
		if !delivery.IsSent() || delivery.MessageID == 0 {
			continue
		}
		if err = c.transport.Delete(ctx, delivery.ChatID, delivery.MessageID); err != nil {
			delivery.Error = "retract: " + err.Error()
			log.With(slog.String("channel", delivery.ChannelID)).Error("retract failed", sl.Err(err))
			continue
		}
		delivery.Status = entity.DeliveryRetracted
		delivery.Error = ""
	}

	// the first retraction is the one that counts: a repeat only finishes
	// what Telegram refused before
	if event.Retraction == nil {
		event.Retraction = &entity.Retraction{
			Reason: reason,
			By:     sender.Username,
			At:     time.Now().UTC(),
		}
	}
	event.LeaseUntil = nil
	if err = c.repo.SetEventRetraction(event.EventID, event.Retraction, event.Deliveries); err != nil {
		if releaseErr := c.repo.ReleaseEvent(event.EventID); releaseErr != nil {
			log.Error("release event", sl.Err(releaseErr))
		}
		return nil, err
	}
	log.With(slog.String("by", sender.Username)).Info("event retracted")
	return event, nil
}

// OutcomeType is the message type of a task outcome.
const OutcomeType = "resolved"

// ResolveTask reports the outcome of a task: one new message in every chat the
// events triaged to it reached, then those events are retracted and marked
// resolved. The outcome is journalled as an event of its own under the dedup
// key resolve:<task key>, so a repeated call sends it only where it did not
// arrive and finishes the retractions Telegram refused before. The events are
// retracted only once the outcome has reached every chat: a chat is never left
// with neither the alert nor its outcome.
func (c *Core) ResolveTask(ctx context.Context, sender *entity.Sender, taskKey, subject, text string) (*entity.TaskOutcome, error) {
	if c.repo == nil {
		return nil, ErrNoRepository
	}
	if c.transport == nil {
		return nil, ErrNoTransport
	}
	taskKey = strings.TrimSpace(taskKey)
	text = strings.TrimSpace(text)
	if taskKey == "" {
		return nil, fmt.Errorf("%w: task_key is required", entity.ErrInvalid)
	}
	if text == "" {
		return nil, fmt.Errorf("%w: text is required", entity.ErrInvalid)
	}
	found, err := c.FindEvents(sender, entity.EventFilter{TaskKey: taskKey, Limit: 500, Ascending: true})
	if err != nil {
		return nil, err
	}
	events := make([]entity.Event, 0, len(found))
	now := time.Now().UTC()
	for i := range found {
		if found[i].Resolves != "" {
			continue
		}
		if found[i].IsLeased(now) {
			return nil, ErrInProgress
		}
		events = append(events, found[i])
	}
	if len(events) == 0 {
		return nil, fmt.Errorf("events of task %s: %w", taskKey, ErrNotFound)
	}

	// the outcome goes where the events went, once per channel, whatever
	// topic each of them came from
	recipients := make([]entity.Channel, 0)
	seen := make(map[string]bool)
	for i := range events {
		for _, delivery := range events[i].Deliveries {
			if delivery.Status != entity.DeliverySent && delivery.Status != entity.DeliveryRetracted {
				continue
			}
			if seen[delivery.ChannelID] {
				continue
			}
			seen[delivery.ChannelID] = true
			recipients = append(recipients, entity.Channel{
				ChannelID: delivery.ChannelID,
				ChatID:    delivery.ChatID,
				ThreadID:  delivery.ThreadID,
			})
		}
	}

	if strings.TrimSpace(subject) == "" {
		subject = taskKey
	}
	topic := events[len(events)-1].Topic
	msg := &entity.Message{
		Topic:    topic,
		Type:     OutcomeType,
		Subject:  subject,
		Text:     text,
		DedupKey: "resolve:" + taskKey,
		Resolves: taskKey,
	}
	result, done, err := c.prepareEvent(msg, topic, sender, recipients)
	if err != nil {
		return nil, err
	}
	if !done {
		c.deliver(ctx, result, msg, topic)
	}
	outcome := &entity.TaskOutcome{TaskKey: taskKey, Result: result, Events: events}
	if result.Status != entity.EventDelivered && result.Status != entity.EventNoRecipients {
		return outcome, nil
	}

	log := c.log.With(slog.String("task_key", taskKey), slog.String("result_id", result.EventID))
	for i := range outcome.Events {
		event := &outcome.Events[i]
		if event.Retraction == nil || leftInChats(event) {
			retracted, retractErr := c.RetractEvent(ctx, sender, event.EventID, "resolved "+taskKey)
			if retractErr != nil {
				log.With(slog.String("event_id", event.EventID)).Error("retract", sl.Err(retractErr))
				continue
			}
			*event = *retracted
		}
		if event.Resolution != nil {
			continue
		}
		resolution := &entity.Resolution{
			TaskKey:  taskKey,
			ResultID: result.EventID,
			By:       sender.Username,
			At:       time.Now().UTC(),
		}
		if err = c.repo.SetEventResolution(event.EventID, resolution); err != nil {
			log.With(slog.String("event_id", event.EventID)).Error("set resolution", sl.Err(err))
			continue
		}
		event.Resolution = resolution
	}
	log.With(slog.Int("events", len(outcome.Events)), slog.Bool("complete", outcome.IsComplete())).Info("task resolved")
	return outcome, nil
}

// leftInChats reports whether a message of the event is still in a chat.
func leftInChats(event *entity.Event) bool {
	for i := range event.Deliveries {
		if event.Deliveries[i].IsSent() && event.Deliveries[i].MessageID != 0 {
			return true
		}
	}
	return false
}

// eventToWork reads an event the sender may triage or retract. One still being
// delivered is refused: the delivery would write over what is recorded now.
func (c *Core) eventToWork(sender *entity.Sender, eventID string) (*entity.Event, error) {
	event, err := c.repo.GetEvent(eventID)
	if err != nil {
		return nil, err
	}
	if event == nil {
		return nil, fmt.Errorf("event %s: %w", eventID, ErrNotFound)
	}
	if !sender.MayRead(event.Topic) {
		return nil, ErrForbidden
	}
	if event.IsLeased(time.Now().UTC()) {
		return nil, ErrInProgress
	}
	return event, nil
}
