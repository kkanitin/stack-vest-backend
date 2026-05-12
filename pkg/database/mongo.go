package database

import (
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

func NewMongoClient(uri string) (*mongo.Client, error) {
	return mongo.Connect(options.Client().ApplyURI(uri))
}

func NewDatabase(client *mongo.Client, name string) *mongo.Database {
	return client.Database(name)
}
