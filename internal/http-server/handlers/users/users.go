package users

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
	GetUsers() ([]entity.User, error)
	GetUser(username string) (*entity.User, error)
	CreateUser(user *entity.User) error
	UpdateUser(user *entity.User) error
	DeleteUser(username string) error
}

func List(logger *slog.Logger, handler Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		log := logger.With(
			sl.Module("handlers.users"),
			slog.String("request_id", middleware.GetReqID(r.Context())),
		)

		result, err := handler.GetUsers()
		if err != nil {
			log.Error("get users", sl.Err(err))
			render.Status(r, http.StatusInternalServerError)
			render.JSON(w, r, response.Error("Failed to get users"))
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
		username := chi.URLParam(r, "username")

		log := logger.With(
			sl.Module("handlers.users"),
			slog.String("request_id", middleware.GetReqID(r.Context())),
			slog.String("username", username),
		)

		user, err := handler.GetUser(username)
		if err != nil {
			log.Error("get user", sl.Err(err))
			render.Status(r, http.StatusInternalServerError)
			render.JSON(w, r, response.Error("Failed to get user"))
			return
		}
		if user == nil {
			render.Status(r, http.StatusNotFound)
			render.JSON(w, r, response.Error("User not found"))
			return
		}

		user.Token = ""
		render.JSON(w, r, response.Ok(user))
	}
}

func Create(logger *slog.Logger, handler Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		log := logger.With(
			sl.Module("handlers.users"),
			slog.String("request_id", middleware.GetReqID(r.Context())),
		)

		var user entity.User
		if err := render.Bind(r, &user); err != nil {
			log.Error("bind user", sl.Err(err))
			render.Status(r, http.StatusBadRequest)
			render.JSON(w, r, response.Error("Failed to decode request"))
			return
		}

		if err := handler.CreateUser(&user); err != nil {
			log.Error("create user", sl.Err(err))
			render.Status(r, http.StatusInternalServerError)
			render.JSON(w, r, response.Error("Failed to create user"))
			return
		}

		log.Info("user created", slog.String("username", user.Username))
		user.Token = ""
		render.Status(r, http.StatusCreated)
		render.JSON(w, r, response.Ok(user))
	}
}

func Update(logger *slog.Logger, handler Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		username := chi.URLParam(r, "username")

		log := logger.With(
			sl.Module("handlers.users"),
			slog.String("request_id", middleware.GetReqID(r.Context())),
			slog.String("username", username),
		)

		var user entity.User
		if err := render.Bind(r, &user); err != nil {
			log.Error("bind user", sl.Err(err))
			render.Status(r, http.StatusBadRequest)
			render.JSON(w, r, response.Error("Failed to decode request"))
			return
		}
		user.Username = username

		if err := handler.UpdateUser(&user); err != nil {
			log.Error("update user", sl.Err(err))
			render.Status(r, http.StatusInternalServerError)
			render.JSON(w, r, response.Error("Failed to update user"))
			return
		}

		log.Info("user updated")
		user.Token = ""
		render.JSON(w, r, response.Ok(user))
	}
}

func Delete(logger *slog.Logger, handler Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		username := chi.URLParam(r, "username")

		log := logger.With(
			sl.Module("handlers.users"),
			slog.String("request_id", middleware.GetReqID(r.Context())),
			slog.String("username", username),
		)

		if err := handler.DeleteUser(username); err != nil {
			log.Error("delete user", sl.Err(err))
			render.Status(r, http.StatusInternalServerError)
			render.JSON(w, r, response.Error("Failed to delete user"))
			return
		}

		log.Info("user deleted")
		render.JSON(w, r, response.Ok(nil))
	}
}
