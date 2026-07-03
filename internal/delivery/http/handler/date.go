package handler

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/kanitin/stackvest/backend/internal/delivery/http/response"
)

const dateLayout = "2006-01-02"

// parseQueryDate reads an optional YYYY-MM-DD query param. An absent param
// yields a zero time with ok=true; a malformed value writes a 400 response and
// returns ok=false so the caller can return immediately.
func parseQueryDate(c *gin.Context, name string) (time.Time, bool) {
	v := c.Query(name)
	if v == "" {
		return time.Time{}, true
	}
	t, err := time.Parse(dateLayout, v)
	if err != nil {
		response.Err(c, http.StatusBadRequest, name+" must be a date in YYYY-MM-DD format")
		return time.Time{}, false
	}
	return t, true
}
