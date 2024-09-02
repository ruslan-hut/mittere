package service

import (
	"fmt"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/render"
	"log/slog"
	"mittere/entity"
	"mittere/internal/lib/api/response"
	"mittere/internal/lib/sl"
	"net/http"
)

type Service interface {
	SendMail(message *entity.MailMessage) (interface{}, error)
	SendEvent(message *entity.EventMessage) (interface{}, error)
}

func SendTestMail(logger *slog.Logger, handler Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {

		log := logger.With(
			sl.Module("handlers.service"),
			slog.String("request_id", middleware.GetReqID(r.Context())),
		)

		var message entity.MailMessage
		if err := render.Bind(r, &message); err != nil {
			log.Error("bind test mail message", sl.Err(err))
			render.Status(r, 400)
			render.JSON(w, r, response.Error(fmt.Sprintf("Failed to decode: %v", err)))
			return
		}

		log = log.With(
			slog.String("to", message.To),
			sl.Secret("message", message.Message),
		)

		data, err := handler.SendMail(&message)
		if err != nil {
			log.Error("send test mail message", sl.Err(err))
			render.Status(r, 204)
			render.JSON(w, r, response.Error(fmt.Sprintf("Failed to send test mail message: %v", err)))
			return
		}
		log.Info("test mail message sent")

		render.JSON(w, r, response.Ok(data))
	}
}

func SendTestEvent(logger *slog.Logger, handler Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {

		log := logger.With(
			sl.Module("handlers.service"),
			slog.String("request_id", middleware.GetReqID(r.Context())),
		)

		var message entity.EventMessage
		if err := render.Bind(r, &message); err != nil {
			log.Error("bind test event message", sl.Err(err))
			render.Status(r, 400)
			render.JSON(w, r, response.Error(fmt.Sprintf("Failed to decode: %v", err)))
			return
		}

		log = log.With(
			slog.String("type", message.Type),
			sl.Secret("username", message.Username),
		)

		data, err := handler.SendEvent(&message)
		if err != nil {
			log.Error("send test event message", sl.Err(err))
			render.Status(r, 204)
			render.JSON(w, r, response.Error(fmt.Sprintf("Failed to send test event message: %v", err)))
			return
		}
		log.Info("test event message sent")

		render.JSON(w, r, response.Ok(data))
	}
}
