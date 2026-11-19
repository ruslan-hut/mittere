package api

import (
	"fmt"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/render"
	"log/slog"
	"mittere/internal/config"
	"mittere/internal/http-server/handlers/channels"
	"mittere/internal/http-server/handlers/events"
	"mittere/internal/http-server/handlers/health"
	"mittere/internal/http-server/handlers/messages"
	"mittere/internal/http-server/handlers/senders"
	"mittere/internal/http-server/handlers/topics"
	"mittere/internal/http-server/middleware/authenticate"
	"mittere/internal/http-server/middleware/timeout"
	"mittere/internal/lib/sl"
	"net"
	"net/http"
	"time"
)

type Server struct {
	conf       *config.Config
	httpServer *http.Server
	log        *slog.Logger
}

type Handler interface {
	authenticate.Authenticate
	messages.Handler
	topics.Handler
	channels.Handler
	senders.Handler
	events.Handler
	health.Handler
}

// Router builds the routing tree. Tests serve it directly.
func Router(log *slog.Logger, handler Handler) http.Handler {
	router := chi.NewRouter()
	// delivery is synchronous, so a request lives as long as the fan out over
	// the recipients of a topic may take
	router.Use(timeout.Timeout(120))
	router.Use(middleware.RequestID)
	router.Use(middleware.Recoverer)
	router.Use(render.SetContentType(render.ContentTypeJSON))
	router.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.Body = http.MaxBytesReader(w, r.Body, 1<<20) // 1 MB
			next.ServeHTTP(w, r)
		})
	})

	// monitoring reaches the bare health state without a token; an admin token
	// adds the counters and the recent failures
	router.With(authenticate.Optional(handler)).Get("/health", health.Get(log, handler))

	router.Group(func(r chi.Router) {
		r.Use(authenticate.New(log, handler))

		// publishing: a send token is enough
		r.Post("/messages", messages.Publish(log, handler))
		r.Route("/tg", func(r chi.Router) {
			r.Get("/test", messages.SendTest(log, handler))
			r.Post("/msg", messages.PublishLegacy(log, handler))
		})

		// administration: an admin token is required
		r.Group(func(r chi.Router) {
			r.Use(authenticate.AdminOnly)

			r.Route("/topics", func(r chi.Router) {
				r.Get("/", topics.List(log, handler))
				r.Post("/", topics.Create(log, handler))
				r.Get("/{code}", topics.Get(log, handler))
				r.Put("/{code}", topics.Update(log, handler))
				r.Delete("/{code}", topics.Delete(log, handler))
			})

			r.Route("/channels", func(r chi.Router) {
				r.Get("/", channels.List(log, handler))
				r.Post("/link", channels.Link(log, handler))
				r.Get("/{id}", channels.Get(log, handler))
				r.Put("/{id}", channels.Update(log, handler))
				r.Delete("/{id}", channels.Delete(log, handler))
			})

			r.Route("/senders", func(r chi.Router) {
				r.Get("/", senders.List(log, handler))
				r.Post("/", senders.Create(log, handler))
				r.Get("/{username}", senders.Get(log, handler))
				r.Put("/{username}", senders.Update(log, handler))
				r.Delete("/{username}", senders.Delete(log, handler))
			})
		})

		// the journal: an admin sees all of it, an agent the events of its
		// topics, which the business layer enforces per event
		r.Group(func(r chi.Router) {
			r.Use(authenticate.AdminOrAgent)

			r.Route("/events", func(r chi.Router) {
				r.Get("/", events.List(log, handler))
				r.Get("/{id}", events.Get(log, handler))
				r.Post("/{id}/triage", events.Triage(log, handler))
				r.Post("/{id}/retract", events.Retract(log, handler))
			})
		})
	})

	return router
}

func New(conf *config.Config, log *slog.Logger, handler Handler) error {

	server := Server{
		conf: conf,
		log:  log.With(sl.Module("api.server")),
	}

	server.httpServer = &http.Server{
		Handler:           Router(log, handler),
		ErrorLog:          slog.NewLogLogger(log.Handler(), slog.LevelError),
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      150 * time.Second,
		IdleTimeout:       60 * time.Second,
		ReadHeaderTimeout: 5 * time.Second,
		MaxHeaderBytes:    1 << 20, // 1 MB
	}

	serverAddress := fmt.Sprintf("%s:%s", conf.Listen.BindIP, conf.Listen.Port)
	listener, err := net.Listen("tcp", serverAddress)
	if err != nil {
		return err
	}

	server.log.Info("starting api server", slog.String("address", serverAddress))

	return server.httpServer.Serve(listener)
}
