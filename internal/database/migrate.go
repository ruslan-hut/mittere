package database

import (
	"context"
	"fmt"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"mittere/entity"
	"time"
)

const migrationV2 = "v2-channels-topics"

// MigrationReport tells what the v1 to v2 migration did.
type MigrationReport struct {
	Applied       bool
	Users         int
	Channels      int
	Topics        int
	Subscriptions int
	Senders       int
}

// Migrate rebuilds the v1 schema as the v2 one. The v1 "subscriptions"
// collection held users that were at once subscriber, recipient and sender; every
// user with a Telegram identifier becomes a private channel, its
// subscription_type becomes a topic, and every user with a token becomes a
// sender. The v1 documents are copied to "users_v1" before they are removed, so
// nothing is lost. The migration records itself and runs only once.
func (m *MongoDB) Migrate(ctx context.Context) (*MigrationReport, error) {
	report := &MigrationReport{}

	done, err := m.migrationApplied(ctx, migrationV2)
	if err != nil {
		return nil, err
	}
	if done {
		return report, nil
	}

	raw, err := m.legacyDocuments(ctx)
	if err != nil {
		return nil, err
	}
	users, err := decodeUsers(raw)
	if err != nil {
		return nil, err
	}
	report.Users = len(users)

	if len(raw) > 0 {
		// the documents are copied as they are, identifier included, so the
		// copy is faithful and a repeated run does not pile up duplicates
		backup := make([]interface{}, 0, len(raw))
		for _, doc := range raw {
			backup = append(backup, doc)
		}
		opts := options.InsertMany().SetOrdered(false)
		if _, err = m.collection(legacyUsersCollection).InsertMany(ctx, backup, opts); err != nil && !mongo.IsDuplicateKeyError(err) {
			return nil, fmt.Errorf("backup of v1 users: %w", err)
		}
	}

	now := time.Now().UTC()
	topics := map[string]bool{entity.TopicDefault: true, entity.TopicAdmin: true}
	channels := make([]entity.Channel, 0, len(users))
	subs := make([]entity.Subscription, 0, len(users))
	senders := make([]entity.Sender, 0)

	for _, user := range users {
		if user.Token != "" {
			scope := entity.ScopeSend
			if user.Role == entity.RoleAdmin {
				scope = entity.ScopeAdmin
			}
			sender := entity.NewSender(user.Username, scope)
			sender.Name = user.Name
			sender.Token = user.Token
			// v1 authenticated a token whatever the state of its user, and
			// state there meant the Telegram subscription; a migrated sender
			// stays active so nothing that worked yesterday gets a 401 today
			sender.State = entity.StateActive
			sender.CreatedAt = now
			senders = append(senders, sender)
		}
		if user.UserID == 0 {
			continue
		}
		channel := entity.NewChannel(user.UserID, 0, entity.ChannelPrivate)
		channel.Title = user.Name
		channel.Username = user.Username
		channel.State = user.State
		if channel.State == "" {
			channel.State = entity.StateAwait
		}
		channel.LinkedAt = now
		channel.LinkedBy = "migration"
		channels = append(channels, channel)

		// every migrated recipient keeps receiving what it received in v1:
		// the default topic carries the untargeted broadcasts, its own
		// subscription type carries the rest, and admins keep the admin topic.
		codes := []string{entity.TopicDefault}
		if user.SubscriptionType != "" {
			if err = entity.ValidateTopicCode(user.SubscriptionType); err == nil {
				codes = append(codes, user.SubscriptionType)
			}
		}
		if user.Role == entity.RoleAdmin {
			codes = append(codes, entity.TopicAdmin)
		}
		for _, code := range codes {
			topics[code] = true
			subs = append(subs, entity.NewSubscription(code, channel.ChannelID, "migration"))
		}
	}

	for code := range topics {
		topic := entity.NewTopic(code, "")
		switch code {
		case entity.TopicDefault:
			topic.Description = "Сообщения без темы, в том числе из устаревшего POST /tg/msg"
		case entity.TopicAdmin:
			topic.Description = "Сообщения устаревшего POST /tg/msg с role=admin"
		}
		if err = m.EnsureTopic(&topic); err != nil {
			return nil, fmt.Errorf("topic %s: %w", code, err)
		}
		report.Topics++
	}

	for i := range channels {
		if err = m.SaveChannel(&channels[i]); err != nil {
			return nil, fmt.Errorf("channel %s: %w", channels[i].ChannelID, err)
		}
		report.Channels++
	}

	for i := range senders {
		if err = m.CreateSender(&senders[i]); err != nil && !mongo.IsDuplicateKeyError(err) {
			return nil, fmt.Errorf("sender %s: %w", senders[i].Username, err)
		}
		report.Senders++
	}

	// the links go in first and the v1 documents, which share this collection,
	// are cleared last: a crash in between leaves the v1 documents in place,
	// so the next run rebuilds everything instead of finding nothing
	for i := range subs {
		if err = m.Subscribe(&subs[i]); err != nil {
			return nil, fmt.Errorf("subscription %s: %w", subs[i].ChannelID, err)
		}
		report.Subscriptions++
	}

	if len(users) > 0 {
		if _, err = m.collection(subscriptionsCollection).DeleteMany(ctx, legacyUserFilter()); err != nil {
			return nil, fmt.Errorf("clearing v1 users: %w", err)
		}
	}

	if err = m.markMigration(ctx, migrationV2, report); err != nil {
		return nil, err
	}
	report.Applied = true
	return report, nil
}

// EnsureDefaults creates the topics the service cannot work without.
func (m *MongoDB) EnsureDefaults() error {
	for _, code := range []string{entity.TopicDefault, entity.TopicAdmin} {
		topic := entity.NewTopic(code, "")
		if err := m.EnsureTopic(&topic); err != nil {
			return err
		}
	}
	return nil
}

func legacyUserFilter() bson.M {
	return bson.M{"username": bson.M{"$exists": true}, "topic_code": bson.M{"$exists": false}}
}

func (m *MongoDB) legacyDocuments(ctx context.Context) ([]bson.Raw, error) {
	cursor, err := m.collection(subscriptionsCollection).Find(ctx, legacyUserFilter())
	if err != nil {
		return nil, err
	}
	docs := make([]bson.Raw, 0)
	if err = cursor.All(ctx, &docs); err != nil {
		return nil, err
	}
	return docs, nil
}

func decodeUsers(docs []bson.Raw) ([]entity.User, error) {
	users := make([]entity.User, 0, len(docs))
	for _, doc := range docs {
		user := entity.User{}
		if err := bson.Unmarshal(doc, &user); err != nil {
			return nil, fmt.Errorf("reading a v1 user: %w", err)
		}
		users = append(users, user)
	}
	return users, nil
}

func (m *MongoDB) migrationApplied(ctx context.Context, name string) (bool, error) {
	result := m.collection(migrationsCollection).FindOne(ctx, bson.M{"_id": name})
	if result.Err() == nil {
		return true, nil
	}
	if err := m.findError(result.Err()); err != nil {
		return false, err
	}
	return false, nil
}

func (m *MongoDB) markMigration(ctx context.Context, name string, report *MigrationReport) error {
	doc := bson.M{
		"_id":           name,
		"applied_at":    time.Now().UTC(),
		"users":         report.Users,
		"channels":      report.Channels,
		"topics":        report.Topics,
		"subscriptions": report.Subscriptions,
		"senders":       report.Senders,
	}
	_, err := m.collection(migrationsCollection).UpdateOne(ctx, bson.M{"_id": name}, bson.M{"$set": doc}, options.Update().SetUpsert(true))
	return err
}
