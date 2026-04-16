package telegram

import (
	"fmt"
	"github.com/go-telegram/bot/models"
	"mittere/entity"
	"mittere/internal/lib/sl"
)

func (b *TgBot) subscribe(update *models.Update) string {
	userId := update.Message.From.ID

	b.mu.Lock()
	defer b.mu.Unlock()

	user := b.getUserLocked(userId)
	if user != nil {
		if user.IsActive() {
			return "Already subscribed"
		}
		if user.IsVerified {
			user.Confirm()
			if err := b.saveUserLocked(user); err != nil {
				return fmt.Sprintf("Error confirming subscription:\n `%v`", err)
			}
			return "Subscription is activated, enjoy"
		}
		return "Awaiting confirmation, send invite code"
	}

	newUser := entity.NewUser(userId, update.Message.From.Username)
	if err := b.saveUserLocked(&newUser); err != nil {
		return fmt.Sprintf("Error registering:\n `%v`", err)
	}
	return fmt.Sprintf("Hello *%v*, you are registered\n To activate notifications, send invite code", update.Message.From.Username)
}

func (b *TgBot) confirmUser(update *models.Update) string {
	userId := update.Message.From.ID

	b.mu.Lock()
	defer b.mu.Unlock()

	user := b.getUserLocked(userId)
	if user == nil {
		return "User not found"
	}
	user.Confirm()
	if err := b.saveUserLocked(user); err != nil {
		return fmt.Sprintf("Error confirming:\n `%v`", err)
	}
	return "Subscription is activated, enjoy"
}

func (b *TgBot) unsubscribe(update *models.Update) string {
	userId := update.Message.From.ID

	b.mu.Lock()
	defer b.mu.Unlock()

	user := b.getUserLocked(userId)
	if user == nil {
		return "User not found"
	}
	user.Disable()
	if err := b.saveUserLocked(user); err != nil {
		return fmt.Sprintf("Error disabling subscription:\n `%v`", err)
	}
	return "Subscription deleted"
}

func (b *TgBot) isAdmin(update *models.Update) bool {
	userId := update.Message.From.ID

	b.mu.RLock()
	defer b.mu.RUnlock()

	user := b.getUserLocked(userId)
	if user == nil {
		return false
	}
	return user.IsAdmin()
}

// saveUserLocked persists a user to cache and database. Caller must hold b.mu.
func (b *TgBot) saveUserLocked(user *entity.User) error {
	b.users[user.UserID] = *user
	if b.database != nil {
		if err := b.database.UpsertUser(user); err != nil {
			b.log.Error("saving user", sl.Err(err))
			return fmt.Errorf("failed to save user")
		}
	}
	return nil
}

// getUserLocked looks up a user by Telegram ID. Caller must hold b.mu (read or write).
func (b *TgBot) getUserLocked(userId int64) *entity.User {
	if user, ok := b.users[userId]; ok {
		return &user
	}
	if b.database != nil {
		user, err := b.database.GetUserByTelegramID(userId)
		if err != nil {
			b.log.Error("getting user", sl.Err(err))
			return nil
		}
		if user != nil {
			b.users[userId] = *user
			return user
		}
	}
	return nil
}

func (b *TgBot) checkInviteCode(code string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	for i, invite := range b.invites {
		if invite == code {
			b.invites = append(b.invites[:i], b.invites[i+1:]...)
			return true
		}
	}
	return false
}
