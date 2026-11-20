package core

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"mittere/entity"
	"mittere/internal/lib/sl"
	"time"
)

// Publish routes a message to the channels subscribed to its topic, sends it,
// and returns the journal record with the outcome for every recipient. The call
// returns only after every delivery has been attempted, so the caller learns
// what actually happened instead of being told the message was accepted.
func (c *Core) Publish(ctx context.Context, msg *entity.Message, sender *entity.Sender) (*entity.Event, error) {
	if c.repo == nil {
		return nil, ErrNoRepository
	}
	if c.transport == nil {
		return nil, ErrNoTransport
	}

	topic := ResolveTopic(msg)
	if err := entity.ValidateTopicCode(topic); err != nil {
		return nil, fmt.Errorf("%w: %s", ErrUnknownTopic, err)
	}
	if sender != nil && !sender.MayPublish(topic) {
		return nil, ErrForbidden
	}
	known, err := c.repo.GetTopic(topic)
	if err != nil {
		return nil, fmt.Errorf("read topic: %w", err)
	}
	if known == nil {
		return nil, fmt.Errorf("%w: %s", ErrUnknownTopic, topic)
	}

	recipients, err := c.recipients(msg, topic)
	if err != nil {
		return nil, err
	}

	event, done, err := c.prepareEvent(msg, topic, sender, recipients)
	if err != nil {
		return nil, err
	}
	if done {
		return event, nil
	}
	c.deliver(ctx, event, msg, topic)
	return event, nil
}

// deliver sends a prepared event to every recipient it has not reached yet and
// records the outcome.
func (c *Core) deliver(ctx context.Context, event *entity.Event, msg *entity.Message, topic string) {
	// the text is rendered from the message as it will be journalled, with the
	// topic already resolved
	rendered := *msg
	rendered.Topic = topic
	text := c.transport.Format(&rendered)
	log := c.log.With(slog.String("event_id", event.EventID), slog.String("topic", topic))

	for i := range event.Deliveries {
		delivery := &event.Deliveries[i]
		if delivery.IsSent() {
			continue
		}
		messageID, attempts, sendErr := c.transport.Send(ctx, delivery.ChatID, delivery.ThreadID, text)
		delivery.Attempts += attempts
		if sendErr != nil {
			delivery.Status = entity.DeliveryFailed
			delivery.Error = sendErr.Error()
			log.With(slog.String("channel", delivery.ChannelID)).Error("delivery failed", sl.Err(sendErr))
		} else {
			sentAt := time.Now().UTC()
			delivery.Status = entity.DeliverySent
			delivery.Error = ""
			delivery.MessageID = messageID
			delivery.SentAt = &sentAt
		}
		// the journal is written after every recipient: if the service dies
		// mid fan out, a retry picks up where it stopped instead of sending
		// everything again
		if err := c.repo.SaveEvent(event); err != nil {
			log.Error("save event", sl.Err(err))
		}
	}

	event.Recalculate()
	finishedAt := time.Now().UTC()
	event.FinishedAt = &finishedAt
	event.LeaseUntil = nil
	if err := c.repo.SaveEvent(event); err != nil {
		log.Error("save event", sl.Err(err))
	}
	log.With(slog.String("status", event.Status), slog.Int("recipients", len(event.Deliveries))).Info("message published")
}

// ResolveTopic picks the topic of a message. Messages without a topic come from
// the legacy endpoint: those addressed to the admin role go to the admin topic,
// the rest to the default one.
func ResolveTopic(msg *entity.Message) string {
	if msg.Topic != "" {
		return msg.Topic
	}
	if msg.Role == entity.RoleAdmin {
		return entity.TopicAdmin
	}
	return entity.TopicDefault
}

// recipients resolves the channels a message goes to. Legacy messages aimed at
// one username go to that person's channels, whatever the topic.
func (c *Core) recipients(msg *entity.Message, topic string) ([]entity.Channel, error) {
	if msg.Username != "" {
		channels, err := c.repo.GetChannelsByUsername(msg.Username)
		if err != nil {
			return nil, fmt.Errorf("read channels of %s: %w", msg.Username, err)
		}
		return channels, nil
	}
	channels, err := c.repo.GetChannelsByTopic(topic)
	if err != nil {
		return nil, fmt.Errorf("read channels of topic %s: %w", topic, err)
	}
	return channels, nil
}

// prepareEvent creates the journal record, or picks up the one left by an
// earlier call with the same dedup key. It reports done when there is nothing
// left to send: a fully delivered event is never sent twice.
func (c *Core) prepareEvent(msg *entity.Message, topic string, sender *entity.Sender, recipients []entity.Channel) (*entity.Event, bool, error) {
	if msg.DedupKey != "" {
		found, err := c.repo.GetEventByDedupKey(msg.DedupKey)
		if err != nil {
			return nil, false, fmt.Errorf("read event by dedup key: %w", err)
		}
		// what is already delivered is never sent again, and neither is what
		// was retracted from the chats
		if found != nil && (found.Status == entity.EventDelivered || found.Retraction != nil) {
			return found, true, nil
		}
	}

	event := &entity.Event{
		EventID:   newID(),
		Topic:     topic,
		Type:      msg.Type,
		Subject:   msg.Subject,
		Text:      msg.Text,
		Payload:   msg.Payload,
		DedupKey:  msg.DedupKey,
		Resolves:  msg.Resolves,
		CreatedAt: time.Now().UTC(),
		Status:    entity.EventPending,
	}
	if sender != nil {
		event.Sender = sender.Username
	}
	if msg.Resolves != "" {
		// the outcome is already worked: it belongs to the task and stays out
		// of the feed of what is left to triage
		event.Triage = &entity.Triage{Verdict: entity.VerdictTask, TaskKey: msg.Resolves, By: event.Sender, At: event.CreatedAt}
	}

	// claiming comes before any sending: a second call with the same dedup key,
	// such as the retry 1C makes after its own timeout, is turned away instead
	// of delivering the message a second time
	previous, err := c.repo.ClaimEvent(event, c.eventLease)
	if err != nil {
		return nil, false, err
	}
	if previous != nil && previous.Retraction != nil {
		// retracted between the lookup above and the claim: nothing goes out
		// again, and the record is put back as it was, without the claim
		previous.LeaseUntil = nil
		if err = c.repo.SaveEvent(previous); err != nil {
			return nil, false, fmt.Errorf("save event: %w", err)
		}
		return previous, true, nil
	}
	if previous != nil {
		// keep the identifier, so a repeated call from 1C stays one event
		event.EventID = previous.EventID
		event.CreatedAt = previous.CreatedAt
		event.Triage = previous.Triage
		event.Retraction = previous.Retraction
		event.Resolution = previous.Resolution
	}

	event.Deliveries = make([]entity.Delivery, 0, len(recipients))
	for _, channel := range recipients {
		delivery := entity.Delivery{
			ChannelID: channel.ChannelID,
			ChatID:    channel.ChatID,
			ThreadID:  channel.ThreadID,
			Status:    entity.DeliveryPending,
		}
		if previous != nil {
			// a retry only covers what did not get through
			for i := range previous.Deliveries {
				if previous.Deliveries[i].ChannelID == channel.ChannelID {
					delivery.Attempts = previous.Deliveries[i].Attempts
					if previous.Deliveries[i].IsSent() {
						delivery = previous.Deliveries[i]
					}
					break
				}
			}
		}
		event.Deliveries = append(event.Deliveries, delivery)
	}

	// what an earlier attempt delivered to a channel that has since left the
	// topic stays in the journal; dropping it would hide a message that went out
	if previous != nil {
		current := make(map[string]bool, len(recipients))
		for _, channel := range recipients {
			current[channel.ChannelID] = true
		}
		for i := range previous.Deliveries {
			if !current[previous.Deliveries[i].ChannelID] && previous.Deliveries[i].IsSent() {
				event.Deliveries = append(event.Deliveries, previous.Deliveries[i])
			}
		}
	}

	if len(event.Deliveries) == 0 {
		event.Status = entity.EventNoRecipients
		finishedAt := time.Now().UTC()
		event.FinishedAt = &finishedAt
		event.LeaseUntil = nil
		if err = c.repo.SaveEvent(event); err != nil {
			return nil, false, fmt.Errorf("save event: %w", err)
		}
		return event, true, nil
	}

	if err = c.repo.SaveEvent(event); err != nil {
		return nil, false, fmt.Errorf("save event: %w", err)
	}
	return event, false, nil
}

func newID() string {
	buf := make([]byte, 12)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(buf)
}
