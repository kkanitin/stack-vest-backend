package response

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

type Meta struct {
	Total            *int `json:"total"`
	Page             *int `json:"page"`
	Size             *int `json:"size"`
	CurrentPageCount *int `json:"currentPageCount"`
}

type envelope struct {
	Result       any     `json:"result"`
	Code         int     `json:"code"`
	Message      string  `json:"message"`
	ErrorMessage *string `json:"errorMessage"`
}

type listEnvelope struct {
	Results      any     `json:"results"`
	Code         int     `json:"code"`
	Message      string  `json:"message"`
	ErrorMessage *string `json:"errorMessage"`
	Meta         Meta    `json:"meta"`
}

func OK(c *gin.Context, result any) {
	c.JSON(http.StatusOK, envelope{
		Result:  result,
		Code:    http.StatusOK,
		Message: "Success",
	})
}

func Created(c *gin.Context, result any) {
	c.JSON(http.StatusCreated, envelope{
		Result:  result,
		Code:    http.StatusCreated,
		Message: "Success",
	})
}

func Err(c *gin.Context, statusCode int, errMsg string) {
	c.JSON(statusCode, envelope{
		Result:       nil,
		Code:         statusCode,
		Message:      "Error",
		ErrorMessage: &errMsg,
	})
}

func OKList(c *gin.Context, results any, meta Meta) {
	c.JSON(http.StatusOK, listEnvelope{
		Results: results,
		Code:    http.StatusOK,
		Message: "Success",
		Meta:    meta,
	})
}
