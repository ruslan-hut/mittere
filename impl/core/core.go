package core

import (
	"fmt"
	"log/slog"
	"mittere/entity"
	"mittere/internal/lib/sl"
)

type Repository interface {
	GetUser(token string) (*entity.User, error)
}

type Core struct {
	repo Repository
	log  *slog.Logger
}

func New(repo Repository, log *slog.Logger) *Core {
	return &Core{
		repo: repo,
		log:  log.With(sl.Module("core")),
	}
}

func (c *Core) SendMail(message *entity.MailMessage) (interface{}, error) {
	return nil, nil
}

func (c *Core) SendEvent(message *entity.EventMessage) (interface{}, error) {
	return nil, nil
}

func (c *Core) AuthenticateByToken(token string) (*entity.User, error) {
	if token == "" {
		return nil, fmt.Errorf("token not provided")
	}
	if c.repo == nil {
		return nil, fmt.Errorf("repository not initialized")
	}
	user, err := c.repo.GetUser(token)
	if err != nil {
		c.log.With(sl.Secret("token", token)).Error("read user data", sl.Err(err))
	}
	if user == nil {
		return nil, fmt.Errorf("user not found")
	}
	return user, nil
}
