package handler

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/kanitin/stackvest/backend/internal/delivery/http/response"
)

const maxPage = 1_000_000

func parsePagination(c *gin.Context) (page, size int, ok bool) {
	page, _ = strconv.Atoi(c.DefaultQuery("page", "1"))
	size, _ = strconv.Atoi(c.DefaultQuery("size", "20"))
	if page < 1 || page > maxPage {
		response.Err(c, http.StatusBadRequest, "page must be between 1 and 1000000")
		return 0, 0, false
	}
	if size < 1 || size > 100 {
		response.Err(c, http.StatusBadRequest, "size must be between 1 and 100")
		return 0, 0, false
	}
	return page, size, true
}
