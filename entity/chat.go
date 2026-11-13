package entity

// ChatInfo is what the bot learns about the chat a command came from.
type ChatInfo struct {
	ChatID        int64
	ThreadID      int
	Kind          string
	Title         string
	Username      string
	ActorID       int64
	ActorUsername string
}

// ChannelID is the identifier of the channel this chat maps to.
func (c *ChatInfo) ChannelID() string {
	return ChannelKey(c.ChatID, c.ThreadID)
}

// IsPrivate reports whether the chat is a private conversation.
func (c *ChatInfo) IsPrivate() bool {
	return c.Kind == ChannelPrivate
}
