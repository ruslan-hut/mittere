package events

import (
	"context"
	"errors"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/render"
	"log/slog"
	"mittere/entity"
	"mittere/internal/http-server/handlers"
	"mittere/internal/lib/api/cont"
	"mittere/internal/lib/api/response"
	"mittere/internal/lib/sl"
	"net/http"
	"strconv"
	"time"
)

// Handler reads the journal and works its events on behalf of a sender, which
// is held to the topics it may read.
type Handler interface {
	GetEvent(sender *entity.Sender, eventID string) (*entity.Event, error)
	FindEvents(sender *entity.Sender, filter entity.EventFilter) ([]entity.Event, error)
	TriageEvent(sender *entity.Sender, eventID string, triage *entity.Triage) (*entity.Event, error)
	RetractEvent(ctx context.Context, sender *entity.Sender, eventID, reason string) (*entity.Event, error)
}

func log(logger *slog.Logger, r *http.Request) *slog.Logger {
	return logger.With(
		sl.Module("handlers.events"),
		slog.String("request_id", middleware.GetReqID(r.Context())),
	)
}

// List serves GET /events with the filters topic, status, from, to, limit,
// triaged and the cursor after.
func List(logger *slog.Logger, handler Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		filter, err := parseFilter(r)
		if err != nil {
			render.Status(r, http.StatusBadRequest)
			render.JSON(w, r, response.Error(err.Error()))
			return
		}
		result, err := handler.FindEvents(cont.GetSender(r.Context()), filter)
		if err != nil {
			fail(w, r, logger, "find events", err)
			return
		}
		render.JSON(w, r, response.Ok(result))
	}
}

// Get serves GET /events/{id}: the journal record of one published message.
func Get(logger *slog.Logger, handler Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		event, err := handler.GetEvent(cont.GetSender(r.Context()), chi.URLParam(r, "id"))
		if err != nil {
			fail(w, r, logger, "get event", err)
			return
		}
		if event == nil {
			render.Status(r, http.StatusNotFound)
			render.JSON(w, r, response.Error("Event not found"))
			return
		}
		render.JSON(w, r, response.Ok(event))
	}
}

type triageRequest struct {
	Verdict string `json:"verdict"`
	TaskKey string `json:"task_key"`
	Note    string `json:"note"`
}

// Triage serves POST /events/{id}/triage: the verdict of an agent on an event.
func Triage(logger *slog.Logger, handler Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var request triageRequest
		if err := render.DecodeJSON(r.Body, &request); err != nil {
			render.Status(r, http.StatusBadRequest)
			render.JSON(w, r, response.Error("Failed to decode request"))
			return
		}
		triage := &entity.Triage{Verdict: request.Verdict, TaskKey: request.TaskKey, Note: request.Note}
		event, err := handler.TriageEvent(cont.GetSender(r.Context()), chi.URLParam(r, "id"), triage)
		if err != nil {
			fail(w, r, logger, "triage event", err)
			return
		}
		render.JSON(w, r, response.Ok(event))
	}
}

type retractRequest struct {
	Reason string `json:"reason"`
}

// Retract serves POST /events/{id}/retract: the messages of an event are
// deleted from the chats. 200 means nothing of it is left in any chat, 207
// that Telegram refused some of the deletions, 409 that the event is being
// delivered or retracted by another call right now.
func Retract(logger *slog.Logger, handler Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var request retractRequest
		if err := render.DecodeJSON(r.Body, &request); err != nil {
			render.Status(r, http.StatusBadRequest)
			render.JSON(w, r, response.Error("Failed to decode request"))
			return
		}
		ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 2*time.Minute)
		defer cancel()
		event, err := handler.RetractEvent(ctx, cont.GetSender(r.Context()), chi.URLParam(r, "id"), request.Reason)
		if err != nil {
			fail(w, r, logger, "retract event", err)
			return
		}
		for i := range event.Deliveries {
			if event.Deliveries[i].IsSent() && event.Deliveries[i].MessageID != 0 {
				render.Status(r, http.StatusMultiStatus)
				res := response.Error("Some messages were not deleted")
				res.Data = event
				render.JSON(w, r, res)
				return
			}
		}
		render.JSON(w, r, response.Ok(event))
	}
}

// fail answers an error of the business layer: a refused topic, a bad request
// or a missing event keep their meaning, anything else is a server error.
func fail(w http.ResponseWriter, r *http.Request, logger *slog.Logger, action string, err error) {
	status := handlers.Status(err, http.StatusInternalServerError)
	message := err.Error()
	if status == http.StatusInternalServerError {
		log(logger, r).Error(action, sl.Err(err))
		message = "Failed to " + action
	}
	if errors.Is(err, entity.ErrForbidden) {
		message = "Topic is not allowed for this token"
	}
	render.Status(r, status)
	render.JSON(w, r, response.Error(message))
}

func parseFilter(r *http.Request) (entity.EventFilter, error) {
	query := r.URL.Query()
	filter := entity.EventFilter{
		Topic:  query.Get("topic"),
		Status: query.Get("status"),
		After:  query.Get("after"),
	}
	if raw := query.Get("triaged"); raw != "" {
		triaged, err := strconv.ParseBool(raw)
		if err != nil {
			return filter, errors.New("triaged must be true or false")
		}
		filter.Triaged = &triaged
		// a feed of what is left to work is read from the oldest
		filter.Ascending = true
	}
	var err error
	if filter.From, err = parseTime(query.Get("from")); err != nil {
		return filter, err
	}
	if filter.To, err = parseTime(query.Get("to")); err != nil {
		return filter, err
	}
	if raw := query.Get("limit"); raw != "" {
		limit, convErr := strconv.ParseInt(raw, 10, 64)
		if convErr != nil {
			return filter, convErr
		}
		filter.Limit = limit
	}
	return filter, nil
}

// parseTime accepts an RFC 3339 timestamp or a plain date.
func parseTime(value string) (time.Time, error) {
	if value == "" {
		return time.Time{}, nil
	}
	if parsed, err := time.Parse(time.RFC3339, value); err == nil {
		return parsed.UTC(), nil
	}
	parsed, err := time.Parse("2006-01-02", value)
	if err != nil {
		return time.Time{}, err
	}
	return parsed.UTC(), nil
}
