package core

import (
	"log/slog"
	"mittere/internal/lib/sl"
)

type Core struct {
	log *slog.Logger
}

func New(log *slog.Logger) *Core {
	return &Core{
		log: log.With(sl.Module("core")),
	}
}

func (c *Core) SendTest(recipient string, message string) (interface{}, error) {
	return nil, nil
}
