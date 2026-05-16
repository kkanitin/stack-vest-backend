package handler

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/kanitin/stackvest/backend/internal/delivery/http/middleware"
	"github.com/kanitin/stackvest/backend/internal/delivery/http/response"
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
		response.Err(c, http.StatusNotFound, "user not found")
		return
	}
	if err != nil {
		slog.ErrorContext(c.Request.Context(), "failed to find user", "email", email, "error", err)
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
		slog.ErrorContext(c.Request.Context(), "failed to create user", "email", email, "error", err)
		response.Err(c, http.StatusInternalServerError, "failed to create user")
		return
	}

	response.Created(c, user)
}
