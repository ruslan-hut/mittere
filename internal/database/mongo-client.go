package database

import (
	"context"
	"errors"
	"fmt"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"mittere/internal/config"
	"time"
)

// Collection names of the v2 schema. The v1 schema kept subscribers, senders
// and recipients as users in a single "subscriptions" collection; the migration
// copies those to "users_v1" and rebuilds them as channels and subscriptions.
const (
	channelsCollection      = "channels"
	topicsCollection        = "topics"
	subscriptionsCollection = "subscriptions"
	sendersCollection       = "senders"
	eventsCollection        = "events"
	linkCodesCollection     = "link_codes"
	migrationsCollection    = "migrations"
	legacyUsersCollection   = "users_v1"

	connectTimeout = 10 * time.Second
	queryTimeout   = 5 * time.Second
)

type MongoDB struct {
	client       *mongo.Client
	database     string
	eventsTTL    time.Duration
	legacyExists bool
}

// NewMongoClient connects to MongoDB and prepares the schema. It returns a nil
// client without an error when MongoDB is disabled in the configuration.
func NewMongoClient(conf *config.Config) (*MongoDB, error) {
	if !conf.Mongo.Enabled {
		return nil, nil
	}
	connectionUri := fmt.Sprintf("mongodb://%s:%s", conf.Mongo.Host, conf.Mongo.Port)
	clientOptions := options.Client().ApplyURI(connectionUri)
	if conf.Mongo.User != "" {
		clientOptions.SetAuth(options.Credential{
			Username:   conf.Mongo.User,
			Password:   conf.Mongo.Password,
			AuthSource: conf.Mongo.Database,
		})
	}

	ctx, cancel := context.WithTimeout(context.Background(), connectTimeout)
	defer cancel()

	client, err := mongo.Connect(ctx, clientOptions)
	if err != nil {
		return nil, fmt.Errorf("mongodb connect error: %w", err)
	}

	pingCtx, pingCancel := context.WithTimeout(context.Background(), queryTimeout)
	defer pingCancel()

	if err = client.Ping(pingCtx, nil); err != nil {
		_ = client.Disconnect(context.Background())
		return nil, fmt.Errorf("mongodb ping error: %w", err)
	}

	return NewMongoDB(client, conf.Mongo.Database, time.Duration(conf.Events.TTLDays)*24*time.Hour), nil
}

// NewMongoDB wraps an already connected client; used by tests.
func NewMongoDB(client *mongo.Client, database string, eventsTTL time.Duration) *MongoDB {
	if eventsTTL <= 0 {
		eventsTTL = 90 * 24 * time.Hour
	}
	return &MongoDB{client: client, database: database, eventsTTL: eventsTTL}
}

func (m *MongoDB) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), connectTimeout)
	defer cancel()
	return m.client.Disconnect(ctx)
}

// Ping reports whether the database answers.
func (m *MongoDB) Ping(ctx context.Context) error {
	return m.client.Ping(ctx, nil)
}

func (m *MongoDB) collection(name string) *mongo.Collection {
	return m.client.Database(m.database).Collection(name)
}

func (m *MongoDB) findError(err error) error {
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil
	}
	return fmt.Errorf("mongodb find error: %w", err)
}

func timeoutCtx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), queryTimeout)
}

// Prepare creates the indexes of the v2 schema. It is safe to call repeatedly.
func (m *MongoDB) Prepare(ctx context.Context) error {
	specs := []struct {
		collection string
		model      mongo.IndexModel
	}{
		{channelsCollection, mongo.IndexModel{
			Keys:    bson.D{{Key: "channel_id", Value: 1}},
			Options: options.Index().SetUnique(true).SetName("channel_id_unique"),
		}},
		{channelsCollection, mongo.IndexModel{
			Keys: bson.D{{Key: "chat_id", Value: 1}},
		}},
		{topicsCollection, mongo.IndexModel{
			Keys:    bson.D{{Key: "code", Value: 1}},
			Options: options.Index().SetUnique(true).SetName("code_unique"),
		}},
		{subscriptionsCollection, mongo.IndexModel{
			Keys:    bson.D{{Key: "topic_code", Value: 1}, {Key: "channel_id", Value: 1}},
			Options: options.Index().SetUnique(true).SetName("topic_channel_unique"),
		}},
		{sendersCollection, mongo.IndexModel{
			Keys:    bson.D{{Key: "username", Value: 1}},
			Options: options.Index().SetUnique(true).SetName("username_unique"),
		}},
		{sendersCollection, mongo.IndexModel{
			Keys: bson.D{{Key: "token", Value: 1}},
		}},
		{eventsCollection, mongo.IndexModel{
			Keys:    bson.D{{Key: "event_id", Value: 1}},
			Options: options.Index().SetUnique(true).SetName("event_id_unique"),
		}},
		{eventsCollection, mongo.IndexModel{
			Keys: bson.D{{Key: "topic", Value: 1}, {Key: "created_at", Value: -1}},
		}},
		{eventsCollection, mongo.IndexModel{
			Keys: bson.D{{Key: "dedup_key", Value: 1}},
			Options: options.Index().SetName("dedup_key_unique").SetUnique(true).
				SetPartialFilterExpression(bson.M{"dedup_key": bson.M{"$exists": true}}),
		}},
		{eventsCollection, mongo.IndexModel{
			Keys:    bson.D{{Key: "created_at", Value: 1}},
			Options: options.Index().SetName("events_ttl").SetExpireAfterSeconds(int32(m.eventsTTL.Seconds())),
		}},
		{linkCodesCollection, mongo.IndexModel{
			Keys:    bson.D{{Key: "code", Value: 1}},
			Options: options.Index().SetUnique(true).SetName("code_unique"),
		}},
		{linkCodesCollection, mongo.IndexModel{
			Keys:    bson.D{{Key: "expires_at", Value: 1}},
			Options: options.Index().SetName("link_codes_ttl").SetExpireAfterSeconds(3600),
		}},
	}
	for _, spec := range specs {
		if _, err := m.collection(spec.collection).Indexes().CreateOne(ctx, spec.model); err != nil {
			return fmt.Errorf("index on %s: %w", spec.collection, err)
		}
	}
	return nil
}
