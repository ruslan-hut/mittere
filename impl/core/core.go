package core

import (
	"log/slog"
	"mittere/entity"
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

func (c *Core) SendMail(message *entity.MailMessage) (interface{}, error) {
	return nil, nil
}

func (c *Core) SendEvent(message *entity.EventMessage) (interface{}, error) {
	return nil, nil
}
