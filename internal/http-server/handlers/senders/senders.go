package senders

import (
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/render"
	"log/slog"
	"mittere/entity"
	"mittere/internal/http-server/handlers"
	"mittere/internal/lib/api/response"
	"mittere/internal/lib/sl"
	"net/http"
)

type Handler interface {
	GetSenders() ([]entity.Sender, error)
	GetSender(username string) (*entity.Sender, error)
	CreateSender(sender *entity.Sender) error
	UpdateSender(sender *entity.Sender) error
	DeleteSender(username string) error
}

func log(logger *slog.Logger, r *http.Request) *slog.Logger {
	return logger.With(
		sl.Module("handlers.senders"),
		slog.String("request_id", middleware.GetReqID(r.Context())),
	)
}

func List(logger *slog.Logger, handler Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		result, err := handler.GetSenders()
		if err != nil {
			log(logger, r).Error("get senders", sl.Err(err))
			render.Status(r, http.StatusInternalServerError)
			render.JSON(w, r, response.Error("Failed to get senders"))
			return
		}
		for i := range result {
			result[i].Token = ""
		}
		render.JSON(w, r, response.Ok(result))
	}
}

func Get(logger *slog.Logger, handler Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sender, err := handler.GetSender(chi.URLParam(r, "username"))
		if err != nil {
			log(logger, r).Error("get sender", sl.Err(err))
			render.Status(r, http.StatusInternalServerError)
			render.JSON(w, r, response.Error("Failed to get sender"))
			return
		}
		if sender == nil {
			render.Status(r, http.StatusNotFound)
			render.JSON(w, r, response.Error("Sender not found"))
			return
		}
		sender.Token = ""
		render.JSON(w, r, response.Ok(sender))
	}
}

// Create adds a sender. The token is returned once, at creation: afterwards it
// is never shown again.
func Create(logger *slog.Logger, handler Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var sender entity.Sender
		if err := render.Bind(r, &sender); err != nil {
			render.Status(r, http.StatusBadRequest)
			render.JSON(w, r, response.Error(err.Error()))
			return
		}
		if err := handler.CreateSender(&sender); err != nil {
			log(logger, r).Error("create sender", sl.Err(err))
			render.Status(r, handlers.Status(err, http.StatusBadRequest))
			render.JSON(w, r, response.Error(err.Error()))
			return
		}
		render.Status(r, http.StatusCreated)
		render.JSON(w, r, response.Ok(sender))
	}
}

func Update(logger *slog.Logger, handler Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		username := chi.URLParam(r, "username")
		// decoded without the defaults Bind would apply: a field the request
		// leaves out must keep its stored value, not be reset
		var sender entity.Sender
		if err := render.DecodeJSON(r.Body, &sender); err != nil {
			render.Status(r, http.StatusBadRequest)
			render.JSON(w, r, response.Error("Failed to decode request"))
			return
		}
		sender.Username = username
		if err := handler.UpdateSender(&sender); err != nil {
			log(logger, r).Error("update sender", sl.Err(err))
			render.Status(r, handlers.Status(err, http.StatusInternalServerError))
			render.JSON(w, r, response.Error(err.Error()))
			return
		}
		sender.Token = ""
		render.JSON(w, r, response.Ok(sender))
	}
}

func Delete(logger *slog.Logger, handler Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := handler.DeleteSender(chi.URLParam(r, "username")); err != nil {
			log(logger, r).Error("delete sender", sl.Err(err))
			render.Status(r, handlers.Status(err, http.StatusInternalServerError))
			render.JSON(w, r, response.Error(err.Error()))
			return
		}
		render.JSON(w, r, response.Ok(nil))
	}
}
