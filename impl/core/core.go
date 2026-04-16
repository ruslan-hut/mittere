package core

import (
	"crypto/subtle"
	"fmt"
	"log/slog"
	"mittere/entity"
	"mittere/internal/lib/sl"
)

type Repository interface {
	GetUser(token string) (*entity.User, error)
}

type MessageService interface {
	SendEventMessage(msg *entity.EventMessage) error
}

type Core struct {
	repo  Repository
	ms    MessageService
	token string
	log   *slog.Logger
}

func New(repo Repository, log *slog.Logger) *Core {
	return &Core{
		repo: repo,
		log:  log.With(sl.Module("core")),
	}
}

func (c *Core) SetMessageService(ms MessageService) {
	c.ms = ms
}

func (c *Core) SetToken(token string) {
	c.token = token
}

func (c *Core) SendEvent(message *entity.EventMessage) (interface{}, error) {
	if c.ms == nil {
		return nil, fmt.Errorf("not set MessageService")
	}
	return nil, c.ms.SendEventMessage(message)
}

func (c *Core) Notify(message *entity.EventMessage) (interface{}, error) {
	if c.ms == nil {
		return nil, fmt.Errorf("not set MessageService")
	}
	return nil, c.ms.SendEventMessage(message)
}

func (c *Core) AuthenticateByToken(token string) (*entity.User, error) {
	if token == "" {
		return nil, fmt.Errorf("token not provided")
	}
	if c.token != "" && subtle.ConstantTimeCompare([]byte(c.token), []byte(token)) == 1 {
		return &entity.User{
			Username: "system",
			Name:     "system",
		}, nil
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
