package handler

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/kanitin/stackvest/backend/internal/delivery/http/middleware"
	"github.com/kanitin/stackvest/backend/internal/delivery/http/response"
	userdomain "github.com/kanitin/stackvest/backend/internal/domain/user"
	useruc "github.com/kanitin/stackvest/backend/internal/usecase/user"
	"github.com/kanitin/stackvest/backend/pkg/logger"
)

type UserHandler struct {
	userUC *useruc.UserUseCase
}

func NewUserHandler(userUC *useruc.UserUseCase) *UserHandler {
	return &UserHandler{userUC: userUC}
}

func (h *UserHandler) RegisterRoutes(rg *gin.RouterGroup) {
	users := rg.Group("/users")
	users.GET("/me", h.getMe)
	users.POST("/me", h.createMe)
}

func (h *UserHandler) getMe(c *gin.Context) {
	email := c.GetString(middleware.EmailKey)

	user, err := h.userUC.FindByEmail(c.Request.Context(), email)
	if errors.Is(err, userdomain.ErrNotFound) {
		response.Err(c, http.StatusNotFound, "user not found")
		return
	}
	if err != nil {
		zap.L().Error("failed to find user", logger.RequestID(c.Request.Context()), zap.String("email", email), zap.Error(err))
		response.Err(c, http.StatusInternalServerError, "failed to find user")
		return
	}

	response.OK(c, user)
}

func (h *UserHandler) createMe(c *gin.Context) {
	email := c.GetString(middleware.EmailKey)
	name := c.GetString(middleware.NameKey)
	picture := c.GetString(middleware.PictureKey)

	user, err := h.userUC.Create(c.Request.Context(), email, name, picture)
	if errors.Is(err, userdomain.ErrAlreadyExists) {
		response.Err(c, http.StatusConflict, "user already exists")
		return
	}
	if err != nil {
		zap.L().Error("failed to create user", logger.RequestID(c.Request.Context()), zap.String("email", email), zap.Error(err))
		response.Err(c, http.StatusInternalServerError, "failed to create user")
		return
	}

	response.Created(c, user)
}
