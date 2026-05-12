package user

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	userdomain "github.com/kanitin/stackvest/backend/internal/domain/user"
)

type MongoRepository struct {
	col *mongo.Collection
}

func NewMongoRepository(db *mongo.Database) *MongoRepository {
	return &MongoRepository{col: db.Collection("users")}
}

func (r *MongoRepository) FindByGoogleID(ctx context.Context, googleID string) (*userdomain.User, error) {
	var u userdomain.User
	if err := r.col.FindOne(ctx, bson.M{"googleId": googleID}).Decode(&u); err != nil {
		return nil, err
	}
	return &u, nil
}

func (r *MongoRepository) Upsert(ctx context.Context, u *userdomain.User) (*userdomain.User, error) {
	now := time.Now()
	_, err := r.col.UpdateOne(ctx,
		bson.M{"googleId": u.GoogleID},
		bson.M{
			"$set": bson.M{
				"email":     u.Email,
				"name":      u.Name,
				"picture":   u.Picture,
				"updatedAt": now,
			},
			"$setOnInsert": bson.M{"createdAt": now},
		},
		options.UpdateOne().SetUpsert(true),
	)
	if err != nil {
		return nil, err
	}
	return r.FindByGoogleID(ctx, u.GoogleID)
}
