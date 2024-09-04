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
)

const (
	usersCollection = "users"
)

type MongoDB struct {
	ctx           context.Context
	clientOptions *options.ClientOptions
	database      string
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
	client := &MongoDB{
		ctx:           context.Background(),
		clientOptions: clientOptions,
		database:      conf.Mongo.Database,
	}
	return client, nil
}

func (m *MongoDB) connect() (*mongo.Client, error) {
	connection, err := mongo.Connect(m.ctx, m.clientOptions)
	if err != nil {
		return nil, fmt.Errorf("mongodb connect error: %w", err)
	}
	return connection, nil
}

func (m *MongoDB) disconnect(connection *mongo.Client) {
	_ = connection.Disconnect(m.ctx)
}

func (m *MongoDB) findError(err error) error {
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil
	}
	return fmt.Errorf("mongodb find error: %w", err)
}

func (m *MongoDB) GetUser(token string) (*entity.User, error) {
	connection, err := m.connect()
	if err != nil {
		return nil, err
	}
	defer m.disconnect(connection)

	collection := connection.Database(m.database).Collection(usersCollection)
	filter := bson.M{"token": token}
	result := collection.FindOne(m.ctx, filter)
	if result.Err() != nil {
		return nil, m.findError(result.Err())
	}
	user := &entity.User{}
	err = result.Decode(user)
	if err != nil {
		return nil, fmt.Errorf("mongodb decode error: %w", err)
	}
	return user, nil
}
