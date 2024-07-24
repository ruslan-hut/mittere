package service

import (
	"fmt"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/render"
	"log/slog"
	"mittere/internal/lib/api/response"
	"mittere/internal/lib/sl"
	"mittere/internal/lib/validate"
	"net/http"
)

type Service interface {
	SendTest(recipient string, message string) (interface{}, error)
}

type TestMessage struct {
	To      string `json:"to" validate:"required,email"`
	Message string `json:"message" validate:"omitempty"`
}

func (t *TestMessage) Bind(_ *http.Request) error {
	return validate.Struct(t)
}

func SendTest(logger *slog.Logger, handler Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {

		log := logger.With(
			sl.Module("handlers.service"),
			slog.String("request_id", middleware.GetReqID(r.Context())),
		)

		var message TestMessage
		if err := render.Bind(r, &message); err != nil {
			log.Error("bind test message", sl.Err(err))
			render.Status(r, 400)
			render.JSON(w, r, response.Error(fmt.Sprintf("Failed to decode: %v", err)))
			return
		}

		log = log.With(
			slog.String("to", message.To),
			sl.Secret("message", message.Message),
		)

		data, err := handler.SendTest(message.To, message.Message)
		if err != nil {
			log.Error("send test message", sl.Err(err))
			render.Status(r, 204)
			render.JSON(w, r, response.Error(fmt.Sprintf("Failed to send test message: %v", err)))
			return
		}
		log.Info("test message sent")

		render.JSON(w, r, response.Ok(data))
	}
}
