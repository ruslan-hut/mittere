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
	usersCollection = "subscriptions"
	connectTimeout  = 10 * time.Second
	queryTimeout    = 5 * time.Second
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

	pingCtx, pingCancel := context.WithTimeout(context.Background(), queryTimeout)
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

func (m *MongoDB) collection() *mongo.Collection {
	return m.client.Database(m.database).Collection(usersCollection)
}

func (m *MongoDB) findError(err error) error {
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil
	}
	return fmt.Errorf("mongodb find error: %w", err)
}

// GetUserByToken returns a user by authentication token
func (m *MongoDB) GetUserByToken(token string) (*entity.User, error) {
	ctx, cancel := context.WithTimeout(context.Background(), queryTimeout)
	defer cancel()

	filter := bson.M{"token": token}
	result := m.collection().FindOne(ctx, filter)
	if result.Err() != nil {
		return nil, m.findError(result.Err())
	}
	user := &entity.User{}
	if err := result.Decode(user); err != nil {
		return nil, fmt.Errorf("mongodb decode error: %w", err)
	}
	return user, nil
}

// GetUsers returns all users
func (m *MongoDB) GetUsers() ([]entity.User, error) {
	ctx, cancel := context.WithTimeout(context.Background(), queryTimeout)
	defer cancel()

	cursor, err := m.collection().Find(ctx, bson.D{})
	if err != nil {
		return nil, err
	}
	var users []entity.User
	if err = cursor.All(ctx, &users); err != nil {
		return nil, err
	}
	return users, nil
}

// GetUserByUsername returns a user by username
func (m *MongoDB) GetUserByUsername(username string) (*entity.User, error) {
	ctx, cancel := context.WithTimeout(context.Background(), queryTimeout)
	defer cancel()

	filter := bson.M{"username": username}
	result := m.collection().FindOne(ctx, filter)
	if result.Err() != nil {
		return nil, m.findError(result.Err())
	}
	user := &entity.User{}
	if err := result.Decode(user); err != nil {
		return nil, fmt.Errorf("mongodb decode error: %w", err)
	}
	return user, nil
}

// GetUserByTelegramID returns a user by Telegram user ID
func (m *MongoDB) GetUserByTelegramID(id int64) (*entity.User, error) {
	ctx, cancel := context.WithTimeout(context.Background(), queryTimeout)
	defer cancel()

	filter := bson.D{{Key: "user_id", Value: id}}
	var user entity.User
	err := m.collection().FindOne(ctx, filter).Decode(&user)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, nil
		}
		return nil, err
	}
	return &user, nil
}

// CreateUser inserts a new user
func (m *MongoDB) CreateUser(user *entity.User) error {
	ctx, cancel := context.WithTimeout(context.Background(), queryTimeout)
	defer cancel()

	_, err := m.collection().InsertOne(ctx, user)
	return err
}

// UpdateUser updates an existing user by username
func (m *MongoDB) UpdateUser(user *entity.User) error {
	ctx, cancel := context.WithTimeout(context.Background(), queryTimeout)
	defer cancel()

	filter := bson.M{"username": user.Username}
	update := bson.M{"$set": user}
	result, err := m.collection().UpdateOne(ctx, filter, update)
	if err != nil {
		return err
	}
	if result.MatchedCount == 0 {
		return fmt.Errorf("user not found")
	}
	return nil
}

// UpsertUser upserts a user by Telegram user ID
func (m *MongoDB) UpsertUser(user *entity.User) error {
	ctx, cancel := context.WithTimeout(context.Background(), queryTimeout)
	defer cancel()

	filter := bson.D{{Key: "user_id", Value: user.UserID}}
	update := bson.M{"$set": user}
	_, err := m.collection().UpdateOne(ctx, filter, update, options.Update().SetUpsert(true))
	return err
}

// DeleteUser deletes a user by username
func (m *MongoDB) DeleteUser(username string) error {
	ctx, cancel := context.WithTimeout(context.Background(), queryTimeout)
	defer cancel()

	filter := bson.M{"username": username}
	result, err := m.collection().DeleteOne(ctx, filter)
	if err != nil {
		return err
	}
	if result.DeletedCount == 0 {
		return fmt.Errorf("user not found")
	}
	return nil
}
