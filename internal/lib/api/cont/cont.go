package cont

import (
	"context"
	"mittere/entity"
)

type ctxKey string

const SenderDataKey ctxKey = "senderData"

// PutSender stores the authenticated sender in the request context.
func PutSender(c context.Context, sender *entity.Sender) context.Context {
	return context.WithValue(c, SenderDataKey, *sender)
}

// GetSender returns the authenticated sender, or an empty one.
func GetSender(c context.Context) *entity.Sender {
	sender, ok := c.Value(SenderDataKey).(entity.Sender)
	if !ok {
		return &entity.Sender{}
	}
	return &sender
}
