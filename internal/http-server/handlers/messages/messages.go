package messages

import (
	"context"
	"errors"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/render"
	"log/slog"
	"mittere/entity"
	"mittere/impl/core"
	"mittere/internal/lib/api/cont"
	"mittere/internal/lib/api/response"
	"mittere/internal/lib/sl"
	"net/http"
	"time"
)

// Handler publishes a message and reports what reached every recipient.
type Handler interface {
	Publish(ctx context.Context, msg *entity.Message, sender *entity.Sender) (*entity.Event, error)
}

// Publish serves POST /messages. The answer is given only after every delivery
// has been attempted, and its status code says what happened:
// 200 everything delivered, 207 some recipients failed, 502 nobody received it,
// 409 the topic has no active channel.
func Publish(logger *slog.Logger, handler Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sender := cont.GetSender(r.Context())

		log := logger.With(
			sl.Module("handlers.messages"),
			slog.String("sender", sender.Username),
			slog.String("request_id", middleware.GetReqID(r.Context())),
		)

		var message entity.Message
		if err := render.Bind(r, &message); err != nil {
			log.Error("bind message", sl.Err(err))
			render.Status(r, http.StatusBadRequest)
			render.JSON(w, r, response.Error("Failed to decode request"))
			return
		}

		publish(w, r, log, handler, &message, sender)
	}
}

// PublishLegacy serves the old POST /tg/msg. A message without a topic goes to
// the default topic, so the notifications 1C sends today keep working.
func PublishLegacy(logger *slog.Logger, handler Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sender := cont.GetSender(r.Context())

		log := logger.With(
			sl.Module("handlers.telegram"),
			slog.String("sender", sender.Username),
			slog.String("request_id", middleware.GetReqID(r.Context())),
		)

		var legacy entity.EventMessage
		if err := render.Bind(r, &legacy); err != nil {
			log.Error("bind message", sl.Err(err))
			render.Status(r, http.StatusBadRequest)
			render.JSON(w, r, response.Error("Failed to decode request"))
			return
		}

		message := legacy.ToMessage()
		publish(w, r, log, handler, message, sender)
	}
}

// SendTest serves the old GET /tg/test.
func SendTest(logger *slog.Logger, handler Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sender := cont.GetSender(r.Context())

		log := logger.With(
			sl.Module("handlers.service"),
			slog.String("sender", sender.Username),
			slog.String("request_id", middleware.GetReqID(r.Context())),
		)

		message := &entity.Message{
			Topic:   r.URL.Query().Get("topic"),
			Type:    "notification",
			Subject: "test",
			Text:    "This is a test, relax",
			Payload: "is this a test?",
		}
		publish(w, r, log, handler, message, sender)
	}
}

func publish(w http.ResponseWriter, r *http.Request, log *slog.Logger, handler Handler, message *entity.Message, sender *entity.Sender) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 2*time.Minute)
	defer cancel()

	event, err := handler.Publish(ctx, message, sender)
	if err != nil {
		log.Error("publish message", sl.Err(err))
		render.Status(r, statusForError(err))
		render.JSON(w, r, response.Error(messageForError(err)))
		return
	}

	render.Status(r, statusForEvent(event.Status))
	if event.Status == entity.EventDelivered {
		render.JSON(w, r, response.Ok(event))
		return
	}
	res := response.Error(statusMessage(event.Status))
	res.Data = event
	render.JSON(w, r, res)
}

func statusForEvent(status string) int {
	switch status {
	case entity.EventDelivered:
		return http.StatusOK
	case entity.EventPartial:
		return http.StatusMultiStatus
	case entity.EventNoRecipients:
		return http.StatusConflict
	default:
		return http.StatusBadGateway
	}
}

func statusMessage(status string) string {
	switch status {
	case entity.EventPartial:
		return "Delivered to some recipients only"
	case entity.EventNoRecipients:
		return "Topic has no active channels"
	default:
		return "Delivery failed"
	}
}

func statusForError(err error) int {
	switch {
	case errors.Is(err, core.ErrUnknownTopic):
		return http.StatusNotFound
	case errors.Is(err, core.ErrForbidden):
		return http.StatusForbidden
	default:
		return http.StatusInternalServerError
	}
}

func messageForError(err error) string {
	switch {
	case errors.Is(err, core.ErrUnknownTopic):
		return err.Error()
	case errors.Is(err, core.ErrForbidden):
		return "Topic is not allowed for this token"
	default:
		return "Failed to send notification"
	}
}
