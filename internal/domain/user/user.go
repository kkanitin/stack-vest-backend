package user

import (
	"context"
	"errors"
	"time"
)

var (
	ErrNotFound      = errors.New("user not found")
	ErrAlreadyExists = errors.New("user already exists")
)

type User struct {
	ID        string    `json:"id"`
	GoogleID  string    `json:"googleId"`
	Email     string    `json:"email"`
	Name      string    `json:"name"`
	Picture   string    `json:"picture"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type Repository interface {
	FindByGoogleID(ctx context.Context, googleID string) (*User, error)
	FindByEmail(ctx context.Context, email string) (*User, error)
	Upsert(ctx context.Context, user *User) (*User, error)
	Create(ctx context.Context, user *User) (*User, error)
}
