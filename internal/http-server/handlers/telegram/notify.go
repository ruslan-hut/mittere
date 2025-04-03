package telegram

import (
	"fmt"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/render"
	"log/slog"
	"mittere/entity"
	"mittere/internal/lib/api/cont"
	"mittere/internal/lib/api/response"
	"mittere/internal/lib/sl"
	"net/http"
)

type Handler interface {
	Notify(message *entity.EventMessage) (interface{}, error)
}

func SendMessage(logger *slog.Logger, handler Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {

		user := cont.GetUser(r.Context())

		log := logger.With(
			sl.Module("handlers.telegram"),
			slog.String("user", user.Username),
			slog.String("request_id", middleware.GetReqID(r.Context())),
		)

		var message entity.EventMessage
		if err := render.Bind(r, &message); err != nil {
			log.Error("bind message", sl.Err(err))
			render.Status(r, 400)
			render.JSON(w, r, response.Error(fmt.Sprintf("Failed to decode: %v", err)))
			return
		}

		log = log.With(
			slog.String("message.type", message.Type),
			sl.Secret("message.username", message.Username),
		)
		message.Sender = user

		data, err := handler.Notify(&message)
		if err != nil {
			log.Error("send notification", sl.Err(err))
			render.Status(r, 204)
			render.JSON(w, r, response.Error(fmt.Sprintf("Failed to send notification: %v", err)))
			return
		}
		log.Info("notification sent")

		render.JSON(w, r, response.Ok(data))
	}
}
