package telegram

import (
	"fmt"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api"
	"mittere/entity"
	"mittere/internal/lib/sl"
)

func (b *TgBot) subscribe(update *tgbotapi.Update) string {
	userId := update.Message.From.ID

	sub := b.getSubscription(userId)
	if sub != nil {
		if sub.IsActive() {
			return "Already subscribed"
		}
		if sub.IsVerified {
			sub.Confirm()
			err := b.updateSubscription(sub)
			if err != nil {
				return fmt.Sprintf("Error confirming subscription:\n `%v`", err)
			}
			return fmt.Sprintf("Subscription is activated, enjoy")
		}
		return "Awaiting confirmation, send invite code"
	}

	subscription := entity.NewSubscription(userId, update.Message.From.UserName)
	err := b.updateSubscription(&subscription)
	if err != nil {
		return fmt.Sprintf("Error confirming subscription:\n `%v`", err)
	}
	return fmt.Sprintf("Hello *%v*, you are registered\n To activate notifications, send invite code", update.Message.From.UserName)
}

func (b *TgBot) confirmSubscription(update *tgbotapi.Update) string {
	userId := update.Message.From.ID
	subscription := b.subscriptions[userId]
	if b.getSubscription(userId) == nil {
		return "Subscription not found"
	}
	subscription.Confirm()
	err := b.updateSubscription(&subscription)
	if err != nil {
		return fmt.Sprintf("Error confirming subscription:\n `%v`", err)
	}
	return fmt.Sprintf("Subscription is activated, enjoy")
}

// updateSubscription updates a subscription, with upsert option
func (b *TgBot) updateSubscription(subscription *entity.Subscription) error {
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
	subscription := b.subscriptions[userId]
	if b.getSubscription(userId) == nil {
		return "Subscription not found"
	}
	subscription.Disable()
	err := b.updateSubscription(&subscription)
	if err != nil {
		return fmt.Sprintf("Error confirming subscription:\n `%v`", err)
	}
	return fmt.Sprintf("Subscription deleted")
}

func (b *TgBot) isAdmin(update *tgbotapi.Update) bool {
	userId := update.Message.From.ID
	subscription := b.subscriptions[userId]
	if b.getSubscription(userId) == nil {
		return false
	}
	return subscription.IsAdmin()
}

func (b *TgBot) getSubscription(userId int) *entity.Subscription {
	for _, subscription := range b.subscriptions {
		if subscription.UserID == userId {
			return &subscription
		}
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
	for i, invite := range b.invites {
		if invite == code {
			// Remove the invite code from the slice
			b.invites = append(b.invites[:i], b.invites[i+1:]...)
			return true
		}
	}
	return false
}
