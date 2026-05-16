package handler

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/kanitin/stackvest/backend/internal/delivery/http/middleware"
	userdomain "github.com/kanitin/stackvest/backend/internal/domain/user"
	useruc "github.com/kanitin/stackvest/backend/internal/usecase/user"
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
		c.JSON(http.StatusNotFound, gin.H{"error": "user not found"})
		return
	}
	if err != nil {
		slog.ErrorContext(c.Request.Context(), "failed to find user", "email", email, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to find user"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"user": user})
}

func (h *UserHandler) createMe(c *gin.Context) {
	email := c.GetString(middleware.EmailKey)
	name := c.GetString(middleware.NameKey)
	picture := c.GetString(middleware.PictureKey)

	user, err := h.userUC.Create(c.Request.Context(), email, name, picture)
	if errors.Is(err, userdomain.ErrAlreadyExists) {
		c.JSON(http.StatusConflict, gin.H{"error": "user already exists"})
		return
	}
	if err != nil {
		slog.ErrorContext(c.Request.Context(), "failed to create user", "email", email, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create user"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"user": user})
}
