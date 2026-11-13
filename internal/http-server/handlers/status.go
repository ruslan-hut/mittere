// Package handlers holds what the HTTP handlers share.
package handlers

import (
	"errors"
	"mittere/entity"
	"net/http"
)

// Status maps an error from the business layer onto an HTTP status code. A
// storage failure must not be reported as 404, so only the sentinel errors
// change the code.
func Status(err error, fallback int) int {
	switch {
	case errors.Is(err, entity.ErrNotFound):
		return http.StatusNotFound
	case errors.Is(err, entity.ErrConflict), errors.Is(err, entity.ErrInProgress):
		return http.StatusConflict
	default:
		return fallback
	}
}
