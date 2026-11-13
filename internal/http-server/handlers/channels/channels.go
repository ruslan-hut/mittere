package channels

import (
	"errors"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/render"
	"log/slog"
	"mittere/entity"
	"mittere/impl/core"
	"mittere/internal/lib/api/cont"
	"mittere/internal/lib/api/response"
	"mittere/internal/lib/sl"
	"net/http"
)

type Handler interface {
	GetChannels() ([]entity.Channel, error)
	GetChannel(channelID string) (*entity.Channel, error)
	UpdateChannel(channel *entity.Channel, by string) error
	DeleteChannel(channelID string) error
	CreateLinkCode(topic, by string) (*entity.LinkCode, error)
}

type linkRequest struct {
	Topic string `json:"topic,omitempty"`
}

func (l *linkRequest) Bind(_ *http.Request) error { return nil }

func log(logger *slog.Logger, r *http.Request) *slog.Logger {
	return logger.With(
		sl.Module("handlers.channels"),
		slog.String("request_id", middleware.GetReqID(r.Context())),
	)
}

func List(logger *slog.Logger, handler Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		result, err := handler.GetChannels()
		if err != nil {
			log(logger, r).Error("get channels", sl.Err(err))
			render.Status(r, http.StatusInternalServerError)
			render.JSON(w, r, response.Error("Failed to get channels"))
			return
		}
		render.JSON(w, r, response.Ok(result))
	}
}

func Get(logger *slog.Logger, handler Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		channel, err := handler.GetChannel(chi.URLParam(r, "id"))
		if err != nil {
			log(logger, r).Error("get channel", sl.Err(err))
			render.Status(r, http.StatusInternalServerError)
			render.JSON(w, r, response.Error("Failed to get channel"))
			return
		}
		if channel == nil {
			render.Status(r, http.StatusNotFound)
			render.JSON(w, r, response.Error("Channel not found"))
			return
		}
		render.JSON(w, r, response.Ok(channel))
	}
}

// Update changes the state and the topic subscriptions of a channel.
func Update(logger *slog.Logger, handler Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		sender := cont.GetSender(r.Context())

		var channel entity.Channel
		if err := render.DecodeJSON(r.Body, &channel); err != nil {
			render.Status(r, http.StatusBadRequest)
			render.JSON(w, r, response.Error("Failed to decode request"))
			return
		}
		channel.ChannelID = id

		if err := handler.UpdateChannel(&channel, sender.Username); err != nil {
			log(logger, r).Error("update channel", sl.Err(err))
			status := http.StatusBadRequest
			if errors.Is(err, core.ErrNotFound) {
				status = http.StatusNotFound
			}
			render.Status(r, status)
			render.JSON(w, r, response.Error(err.Error()))
			return
		}
		updated, err := handler.GetChannel(id)
		if err != nil {
			render.JSON(w, r, response.Ok(nil))
			return
		}
		render.JSON(w, r, response.Ok(updated))
	}
}

func Delete(logger *slog.Logger, handler Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := handler.DeleteChannel(chi.URLParam(r, "id")); err != nil {
			log(logger, r).Error("delete channel", sl.Err(err))
			render.Status(r, http.StatusNotFound)
			render.JSON(w, r, response.Error(err.Error()))
			return
		}
		render.JSON(w, r, response.Ok(nil))
	}
}

// Link issues a code to be sent in the chat that should receive a topic. The
// chat identifier is never typed by hand: the bot reads it from the message.
func Link(logger *slog.Logger, handler Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sender := cont.GetSender(r.Context())

		var request linkRequest
		if r.ContentLength > 0 {
			if err := render.Bind(r, &request); err != nil {
				render.Status(r, http.StatusBadRequest)
				render.JSON(w, r, response.Error("Failed to decode request"))
				return
			}
		}

		code, err := handler.CreateLinkCode(request.Topic, sender.Username)
		if err != nil {
			log(logger, r).Error("create link code", sl.Err(err))
			status := http.StatusInternalServerError
			if errors.Is(err, core.ErrUnknownTopic) {
				status = http.StatusNotFound
			}
			render.Status(r, status)
			render.JSON(w, r, response.Error(err.Error()))
			return
		}
		render.Status(r, http.StatusCreated)
		render.JSON(w, r, response.Ok(code))
	}
}
