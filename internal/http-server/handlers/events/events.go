package events

import (
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/render"
	"log/slog"
	"mittere/entity"
	"mittere/internal/lib/api/response"
	"mittere/internal/lib/sl"
	"net/http"
	"strconv"
	"time"
)

type Handler interface {
	GetEvent(eventID string) (*entity.Event, error)
	FindEvents(filter entity.EventFilter) ([]entity.Event, error)
}

func log(logger *slog.Logger, r *http.Request) *slog.Logger {
	return logger.With(
		sl.Module("handlers.events"),
		slog.String("request_id", middleware.GetReqID(r.Context())),
	)
}

// List serves GET /events with the filters topic, status, from, to and limit.
func List(logger *slog.Logger, handler Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		filter, err := parseFilter(r)
		if err != nil {
			render.Status(r, http.StatusBadRequest)
			render.JSON(w, r, response.Error(err.Error()))
			return
		}
		result, err := handler.FindEvents(filter)
		if err != nil {
			log(logger, r).Error("find events", sl.Err(err))
			render.Status(r, http.StatusInternalServerError)
			render.JSON(w, r, response.Error("Failed to get events"))
			return
		}
		render.JSON(w, r, response.Ok(result))
	}
}

// Get serves GET /events/{id}: the journal record of one published message.
func Get(logger *slog.Logger, handler Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		event, err := handler.GetEvent(chi.URLParam(r, "id"))
		if err != nil {
			log(logger, r).Error("get event", sl.Err(err))
			render.Status(r, http.StatusInternalServerError)
			render.JSON(w, r, response.Error("Failed to get event"))
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

func parseFilter(r *http.Request) (entity.EventFilter, error) {
	query := r.URL.Query()
	filter := entity.EventFilter{
		Topic:  query.Get("topic"),
		Status: query.Get("status"),
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
