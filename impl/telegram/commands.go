package telegram

import (
	"fmt"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api"
	"mittere/entity"
	"mittere/internal/lib/sl"
)

func (b *TgBot) subscribe(update *tgbotapi.Update) string {
	userId := update.Message.From.ID
	if b.getSubscription(userId) != nil {
		return "Already subscribed"
	}
	subscription := entity.NewSubscription(update.Message.From.ID, update.Message.From.UserName)
	b.subscriptions[update.Message.From.ID] = subscription
	if b.database != nil {
		err := b.database.AddSubscription(&subscription)
		if err != nil {
			b.log.Error("adding subscription", sl.Err(err))
			return fmt.Sprintf("Error adding subscription:\n `%v`", err)
		}
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
	if b.database != nil {
		err := b.database.UpdateSubscription(&subscription)
		if err != nil {
			b.log.Error("updating subscription", sl.Err(err))
			return fmt.Sprintf("Error updating subscription:\n `%v`", err)
		}
	}
	return fmt.Sprintf("Subscription is activated, enjoy")
}

func (b *TgBot) deleteSubscription(update *tgbotapi.Update) string {
	userId := update.Message.From.ID
	subscription := b.subscriptions[userId]
	if b.getSubscription(userId) == nil {
		return "Subscription not found"
	}
	if b.database != nil {
		err := b.database.DeleteSubscription(&subscription)
		if err != nil {
			b.log.Error("deleting subscription", sl.Err(err))
			return fmt.Sprintf("Error deleting subscription:\n `%v`", err)
		}
	}
	delete(b.subscriptions, userId)
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
	return nil
}

func (b *TgBot) checkInviteCode(code string) bool {
	for _, invite := range b.invites {
		if invite == code {
			return true
		}
	}
	return false
}
