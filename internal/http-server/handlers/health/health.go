package health

import (
	"context"
	"github.com/go-chi/render"
	"log/slog"
	"mittere/impl/core"
	"mittere/internal/lib/api/cont"
	"mittere/internal/lib/api/response"
	"net/http"
)

type Handler interface {
	GetHealth(ctx context.Context, detailed bool) *core.Health
}

// Get serves GET /health. Without a token it answers with the bare state, so
// monitoring can watch the service; an admin token also gets the counters and
// the recent delivery failures.
func Get(_ *slog.Logger, handler Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sender := cont.GetSender(r.Context())
		result := handler.GetHealth(r.Context(), sender.IsAdmin())
		if result.Status != "ok" {
			render.Status(r, http.StatusServiceUnavailable)
		}
		render.JSON(w, r, response.Ok(result))
	}
}
