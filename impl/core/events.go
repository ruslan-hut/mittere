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
