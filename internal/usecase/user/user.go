package user

import (
	"context"

	userdomain "github.com/kanitin/stackvest/backend/internal/domain/user"
)

type UserUseCase struct {
	userRepo userdomain.Repository
}

func NewUserUseCase(userRepo userdomain.Repository) *UserUseCase {
	return &UserUseCase{userRepo: userRepo}
}

func (uc *UserUseCase) FindByEmail(ctx context.Context, email string) (*userdomain.User, error) {
	return uc.userRepo.FindByEmail(ctx, email)
}

func (uc *UserUseCase) Create(ctx context.Context, email, name, picture string) (*userdomain.User, error) {
	return uc.userRepo.Create(ctx, &userdomain.User{
		Email:   email,
		Name:    name,
		Picture: picture,
	})
}
