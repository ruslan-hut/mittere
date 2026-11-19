package authenticate

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
	"strings"
	"time"
)

type Authenticate interface {
	AuthenticateByToken(token string) (*entity.Sender, error)
}

func New(log *slog.Logger, auth Authenticate) func(next http.Handler) http.Handler {
	mod := sl.Module("middleware.authenticate")
	log.With(mod).Info("authenticate middleware initialized")

	return func(next http.Handler) http.Handler {

		fn := func(w http.ResponseWriter, r *http.Request) {
			id := middleware.GetReqID(r.Context())
			remote := r.RemoteAddr
			// if the request is coming from a proxy, use the X-Forwarded-For header
			xRemote := r.Header.Get("X-Forwarded-For")
			if xRemote != "" {
				remote = xRemote
			}
			logger := log.With(
				mod,
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
				slog.String("remote_addr", remote),
				slog.String("request_id", id),
			)
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)

			t1 := time.Now()
			defer func() {
				logger.With(
					slog.Int("status", ww.Status()),
					slog.Int("size", ww.BytesWritten()),
					slog.Float64("duration", time.Since(t1).Seconds()),
				).Info("incoming request")
			}()

			header := r.Header.Get("Authorization")
			if len(header) == 0 {
				logger = logger.With(sl.Err(fmt.Errorf("authorization header not found")))
				authFailed(ww, r, "Authorization header not found")
				return
			}
			const bearerPrefix = "Bearer "
			if !strings.HasPrefix(header, bearerPrefix) {
				logger = logger.With(sl.Err(fmt.Errorf("invalid authorization header format")))
				authFailed(ww, r, "Invalid authorization header")
				return
			}
			token := strings.TrimPrefix(header, bearerPrefix)
			if len(token) == 0 {
				logger = logger.With(sl.Err(fmt.Errorf("token not found")))
				authFailed(ww, r, "Token not found")
				return
			}
			logger = logger.With(sl.Secret("token", token))

			if auth == nil {
				authFailed(ww, r, "Unauthorized: authentication not enabled")
				return
			}

			sender, err := auth.AuthenticateByToken(token)
			if err != nil {
				logger = logger.With(sl.Err(err))
				authFailed(ww, r, "Unauthorized: token not found")
				return
			}
			logger = logger.With(
				slog.String("sender", sender.Username),
				slog.String("scope", sender.Scope),
			)
			ctx := cont.PutSender(r.Context(), sender)

			ww.Header().Set("X-Request-ID", id)
			next.ServeHTTP(ww, r.WithContext(ctx))
		}

		return http.HandlerFunc(fn)
	}
}

// AdminOnly rejects senders that hold a send token: publishing a message and
// administering the service are two different rights.
func AdminOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sender := cont.GetSender(r.Context())
		if !sender.IsAdmin() {
			render.Status(r, http.StatusForbidden)
			render.JSON(w, r, response.Error("Admin token required"))
			return
		}
		next.ServeHTTP(w, r)
	})
}

// AdminOrAgent lets through an administrator and an agent, which reads and
// works the journal of its own topics only.
func AdminOrAgent(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sender := cont.GetSender(r.Context())
		if !sender.IsAdmin() && !sender.IsAgent() {
			render.Status(r, http.StatusForbidden)
			render.JSON(w, r, response.Error("Admin or agent token required"))
			return
		}
		next.ServeHTTP(w, r)
	})
}

func authFailed(w http.ResponseWriter, r *http.Request, message string) {
	render.Status(r, http.StatusUnauthorized)
	render.JSON(w, r, response.Error(message))
}

// Optional authenticates when a token is present and lets the request through
// either way. It serves the health endpoint, which monitoring reads without a
// token while an administrator gets the full picture.
func Optional(auth Authenticate) func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			const bearerPrefix = "Bearer "
			header := r.Header.Get("Authorization")
			if auth != nil && strings.HasPrefix(header, bearerPrefix) {
				token := strings.TrimPrefix(header, bearerPrefix)
				if sender, err := auth.AuthenticateByToken(token); err == nil {
					r = r.WithContext(cont.PutSender(r.Context(), sender))
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}
