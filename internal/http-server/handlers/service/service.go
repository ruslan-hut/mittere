package service

import (
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/render"
	"log/slog"
	"mittere/entity"
	"mittere/internal/lib/api/cont"
	"mittere/internal/lib/api/response"
	"mittere/internal/lib/sl"
	"net/http"
	"time"
)

type Service interface {
	SendEvent(message *entity.EventMessage) (interface{}, error)
}

func SendTestEvent(logger *slog.Logger, handler Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {

		user := cont.GetUser(r.Context())

		log := logger.With(
			sl.Module("handlers.service"),
			slog.String("user", user.Username),
			slog.String("request_id", middleware.GetReqID(r.Context())),
		)

		message := entity.EventMessage{
			Type:     "notification",
			Subject:  "test",
			Time:     time.Now(),
			Username: "Someone",
			Text:     "This is a test, relax",
			Payload:  "is this a test?",
		}

		data, err := handler.SendEvent(&message)
		if err != nil {
			log.Error("send test event message", sl.Err(err))
			render.Status(r, http.StatusInternalServerError)
			render.JSON(w, r, response.Error("Failed to send test event message"))
			return
		}
		log.Info("test event message sent")

		render.JSON(w, r, response.Ok(data))
	}
}
