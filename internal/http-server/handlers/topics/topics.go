package topics

import (
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/render"
	"log/slog"
	"mittere/entity"
	"mittere/internal/lib/api/response"
	"mittere/internal/lib/sl"
	"net/http"
)

type Handler interface {
	GetTopics() ([]entity.Topic, error)
	GetTopic(code string) (*entity.Topic, error)
	CreateTopic(topic *entity.Topic) error
	UpdateTopic(topic *entity.Topic) error
	DeleteTopic(code string) error
}

func log(logger *slog.Logger, r *http.Request) *slog.Logger {
	return logger.With(
		sl.Module("handlers.topics"),
		slog.String("request_id", middleware.GetReqID(r.Context())),
	)
}

func List(logger *slog.Logger, handler Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		result, err := handler.GetTopics()
		if err != nil {
			log(logger, r).Error("get topics", sl.Err(err))
			render.Status(r, http.StatusInternalServerError)
			render.JSON(w, r, response.Error("Failed to get topics"))
			return
		}
		render.JSON(w, r, response.Ok(result))
	}
}

func Get(logger *slog.Logger, handler Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		code := chi.URLParam(r, "code")
		topic, err := handler.GetTopic(code)
		if err != nil {
			log(logger, r).Error("get topic", sl.Err(err))
			render.Status(r, http.StatusInternalServerError)
			render.JSON(w, r, response.Error("Failed to get topic"))
			return
		}
		if topic == nil {
			render.Status(r, http.StatusNotFound)
			render.JSON(w, r, response.Error("Topic not found"))
			return
		}
		render.JSON(w, r, response.Ok(topic))
	}
}

func Create(logger *slog.Logger, handler Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var topic entity.Topic
		if err := render.Bind(r, &topic); err != nil {
			render.Status(r, http.StatusBadRequest)
			render.JSON(w, r, response.Error(err.Error()))
			return
		}
		if err := handler.CreateTopic(&topic); err != nil {
			log(logger, r).Error("create topic", sl.Err(err))
			render.Status(r, http.StatusBadRequest)
			render.JSON(w, r, response.Error(err.Error()))
			return
		}
		render.Status(r, http.StatusCreated)
		render.JSON(w, r, response.Ok(topic))
	}
}

func Update(logger *slog.Logger, handler Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		code := chi.URLParam(r, "code")
		var topic entity.Topic
		topic.Code = code
		if err := render.Bind(r, &topic); err != nil {
			render.Status(r, http.StatusBadRequest)
			render.JSON(w, r, response.Error(err.Error()))
			return
		}
		topic.Code = code
		if err := handler.UpdateTopic(&topic); err != nil {
			log(logger, r).Error("update topic", sl.Err(err))
			render.Status(r, http.StatusNotFound)
			render.JSON(w, r, response.Error(err.Error()))
			return
		}
		render.JSON(w, r, response.Ok(topic))
	}
}

func Delete(logger *slog.Logger, handler Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		code := chi.URLParam(r, "code")
		if err := handler.DeleteTopic(code); err != nil {
			log(logger, r).Error("delete topic", sl.Err(err))
			render.Status(r, http.StatusBadRequest)
			render.JSON(w, r, response.Error(err.Error()))
			return
		}
		render.JSON(w, r, response.Ok(nil))
	}
}
