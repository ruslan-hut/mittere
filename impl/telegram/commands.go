package telegram

import (
	"fmt"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api"
	"mittere/entity"
	"mittere/internal/lib/sl"
)

func (b *TgBot) subscribe(update *tgbotapi.Update) string {
	userId := update.Message.From.ID

	b.mu.Lock()
	defer b.mu.Unlock()

	sub := b.getSubscriptionLocked(userId)
	if sub != nil {
		if sub.IsActive() {
			return "Already subscribed"
		}
		if sub.IsVerified {
			sub.Confirm()
			err := b.updateSubscriptionLocked(sub)
			if err != nil {
				return fmt.Sprintf("Error confirming subscription:\n `%v`", err)
			}
			return fmt.Sprintf("Subscription is activated, enjoy")
		}
		return "Awaiting confirmation, send invite code"
	}

	subscription := entity.NewSubscription(userId, update.Message.From.UserName)
	err := b.updateSubscriptionLocked(&subscription)
	if err != nil {
		return fmt.Sprintf("Error confirming subscription:\n `%v`", err)
	}
	return fmt.Sprintf("Hello *%v*, you are registered\n To activate notifications, send invite code", update.Message.From.UserName)
}

func (b *TgBot) confirmSubscription(update *tgbotapi.Update) string {
	userId := update.Message.From.ID

	b.mu.Lock()
	defer b.mu.Unlock()

	sub := b.getSubscriptionLocked(userId)
	if sub == nil {
		return "Subscription not found"
	}
	sub.Confirm()
	err := b.updateSubscriptionLocked(sub)
	if err != nil {
		return fmt.Sprintf("Error confirming subscription:\n `%v`", err)
	}
	return fmt.Sprintf("Subscription is activated, enjoy")
}

// updateSubscriptionLocked updates a subscription, with upsert option. Caller must hold b.mu.
func (b *TgBot) updateSubscriptionLocked(subscription *entity.Subscription) error {
	b.subscriptions[subscription.UserID] = *subscription
	if b.database != nil {
		err := b.database.UpdateSubscription(subscription)
		if err != nil {
			b.log.Error("updating subscription", sl.Err(err))
			return fmt.Errorf("failed to update subscription")
		}
	}
	return nil
}

func (b *TgBot) deleteSubscription(update *tgbotapi.Update) string {
	userId := update.Message.From.ID

	b.mu.Lock()
	defer b.mu.Unlock()

	sub := b.getSubscriptionLocked(userId)
	if sub == nil {
		return "Subscription not found"
	}
	sub.Disable()
	err := b.updateSubscriptionLocked(sub)
	if err != nil {
		return fmt.Sprintf("Error disabling subscription:\n `%v`", err)
	}
	return fmt.Sprintf("Subscription deleted")
}

func (b *TgBot) isAdmin(update *tgbotapi.Update) bool {
	userId := update.Message.From.ID

	b.mu.RLock()
	defer b.mu.RUnlock()

	sub := b.getSubscriptionLocked(userId)
	if sub == nil {
		return false
	}
	return sub.IsAdmin()
}

// getSubscriptionLocked looks up a subscription by userId. Caller must hold b.mu (read or write).
func (b *TgBot) getSubscriptionLocked(userId int) *entity.Subscription {
	if sub, ok := b.subscriptions[userId]; ok {
		return &sub
	}
	// check in database
	if b.database != nil {
		subscription, err := b.database.GetSubscription(userId)
		if err != nil {
			b.log.Error("getting subscription", sl.Err(err))
			return nil
		}
		if subscription != nil {
			b.subscriptions[userId] = *subscription
			return subscription
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
