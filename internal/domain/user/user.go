package user

import (
	"context"
	"time"
)

type User struct {
	ID        string    `bson:"_id,omitempty" json:"id"`
	GoogleID  string    `bson:"googleId" json:"googleId"`
	Email     string    `bson:"email" json:"email"`
	Name      string    `bson:"name" json:"name"`
	Picture   string    `bson:"picture" json:"picture"`
	CreatedAt time.Time `bson:"createdAt" json:"createdAt"`
	UpdatedAt time.Time `bson:"updatedAt" json:"updatedAt"`
}

type Repository interface {
	FindByGoogleID(ctx context.Context, googleID string) (*User, error)
	Upsert(ctx context.Context, user *User) (*User, error)
}
