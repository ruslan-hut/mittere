package database

import (
	"context"
	"errors"
	"fmt"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"mittere/entity"
	"mittere/internal/config"
	"time"
)

const (
	usersCollection         = "users"
	subscriptionsCollection = "subscriptions"
	connectTimeout          = 10 * time.Second
	pingTimeout             = 5 * time.Second
)

type MongoDB struct {
	client   *mongo.Client
	database string
}

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

	pingCtx, pingCancel := context.WithTimeout(context.Background(), pingTimeout)
	defer pingCancel()

	if err = client.Ping(pingCtx, nil); err != nil {
		_ = client.Disconnect(context.Background())
		return nil, fmt.Errorf("mongodb ping error: %w", err)
	}

	return &MongoDB{
		client:   client,
		database: conf.Mongo.Database,
	}, nil
}

func (m *MongoDB) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), connectTimeout)
	defer cancel()
	return m.client.Disconnect(ctx)
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

func (m *MongoDB) GetUser(token string) (*entity.User, error) {
	ctx, cancel := context.WithTimeout(context.Background(), pingTimeout)
	defer cancel()

	filter := bson.M{"token": token}
	result := m.collection(usersCollection).FindOne(ctx, filter)
	if result.Err() != nil {
		return nil, m.findError(result.Err())
	}
	user := &entity.User{}
	err := result.Decode(user)
	if err != nil {
		return nil, fmt.Errorf("mongodb decode error: %w", err)
	}
	return user, nil
}

// GetSubscriptions returns all subscriptions
func (m *MongoDB) GetSubscriptions() ([]entity.Subscription, error) {
	ctx, cancel := context.WithTimeout(context.Background(), pingTimeout)
	defer cancel()

	filter := bson.D{}
	cursor, err := m.collection(subscriptionsCollection).Find(ctx, filter)
	if err != nil {
		return nil, err
	}
	var subscriptions []entity.Subscription
	if err = cursor.All(ctx, &subscriptions); err != nil {
		return nil, err
	}
	return subscriptions, nil
}

// GetSubscription returns a subscription by user id
func (m *MongoDB) GetSubscription(id int) (*entity.Subscription, error) {
	ctx, cancel := context.WithTimeout(context.Background(), pingTimeout)
	defer cancel()

	filter := bson.D{{Key: "user_id", Value: id}}
	var subscription entity.Subscription
	err := m.collection(subscriptionsCollection).FindOne(ctx, filter).Decode(&subscription)
	if err != nil {
		return nil, err
	}
	return &subscription, nil
}

// UpdateSubscription updates a subscription
func (m *MongoDB) UpdateSubscription(subscription *entity.Subscription) error {
	ctx, cancel := context.WithTimeout(context.Background(), pingTimeout)
	defer cancel()

	filter := bson.D{{Key: "user_id", Value: subscription.UserID}}
	update := bson.M{"$set": subscription}
	_, err := m.collection(subscriptionsCollection).UpdateOne(ctx, filter, update, options.Update().SetUpsert(true))
	if err != nil {
		return err
	}
	return nil
}
