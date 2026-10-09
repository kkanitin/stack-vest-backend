package handler

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/kanitin/stackvest/backend/internal/delivery/http/middleware"
	"github.com/kanitin/stackvest/backend/internal/delivery/http/response"
	portfoliodomain "github.com/kanitin/stackvest/backend/internal/domain/portfolio"
	portfoliouc "github.com/kanitin/stackvest/backend/internal/usecase/portfolio"
	"github.com/kanitin/stackvest/backend/pkg/logger"
)

// --- Transactions (the ledger) ---

// createTransactionRequest is a new buy or sell. Quantity and price are pointers so a
// missing field is rejected while price 0 stays valid; fee is optional (default 0).
type createTransactionRequest struct {
	Symbol   string   `json:"symbol" binding:"required"`
	Name     string   `json:"name" binding:"required"`
	Side     string   `json:"side" binding:"required,oneof=buy sell"`
	Quantity *float64 `json:"quantity" binding:"required,gt=0"`
	Price    *float64 `json:"price" binding:"required,gte=0"`
	Fee      *float64 `json:"fee" binding:"omitempty,gte=0"`
	Note     *string  `json:"note" binding:"omitempty,max=500"`
	Date     string   `json:"date" binding:"required"` // YYYY-MM-DD, not in the future (checked in the use case)
}

// updateTransactionRequest changes any of side, quantity, price, fee, note and date. The
// symbol cannot change.
type updateTransactionRequest struct {
	Side     *string  `json:"side" binding:"omitempty,oneof=buy sell"`
	Quantity *float64 `json:"quantity" binding:"omitempty,gt=0"`
	Price    *float64 `json:"price" binding:"omitempty,gte=0"`
	Fee      *float64 `json:"fee" binding:"omitempty,gte=0"`
	Note     *string  `json:"note" binding:"omitempty,max=500"`
	Date     *string  `json:"date"`
}

type listTransactionsQuery struct {
	Symbol string `form:"symbol"`
}

// transactionError writes the response for an error from a ledger use case and reports
// whether it handled err. Business-rule refusals are 409 (matching the rest of the
// codebase), malformed input 400, and a missing or not-owned portfolio or transaction 404.
func (h *PortfolioHandler) transactionError(c *gin.Context, err error, action string, fields ...zap.Field) {
	var insufficient portfoliodomain.ErrInsufficientShares
	switch {
	case errors.Is(err, portfoliodomain.ErrPortfolioNotFound):
		response.Err(c, http.StatusNotFound, "portfolio not found")
	case errors.Is(err, portfoliodomain.ErrTransactionNotFound):
		response.Err(c, http.StatusNotFound, "transaction not found")
	case errors.As(err, &insufficient):
		response.Err(c, http.StatusConflict, insufficient.Error())
	case errors.Is(err, portfoliodomain.ErrPositionLimitReached):
		response.Err(c, http.StatusConflict, "position limit reached")
	case errors.Is(err, portfoliodomain.ErrFutureDate), errors.Is(err, portfoliodomain.ErrInvalidTransaction):
		response.Err(c, http.StatusBadRequest, err.Error())
	default:
		zap.L().Error(
			"failed to "+action,
			append([]zap.Field{
				logger.RequestID(c.Request.Context()), zap.String("email", c.GetString(middleware.EmailKey)),
				zap.String("id", c.Param("id")), zap.Error(err),
			}, fields...)...,
		)
		response.Err(c, http.StatusInternalServerError, "failed to "+action)
	}
}

func (h *PortfolioHandler) listTransactions(c *gin.Context) {
	page, size, ok := parsePagination(c)
	if !ok {
		return
	}
	var q listTransactionsQuery
	if err := c.ShouldBindQuery(&q); err != nil {
		response.Err(c, http.StatusBadRequest, err.Error())
		return
	}

	email := c.GetString(middleware.EmailKey)
	txs, total, err := h.uc.ListTransactions(c.Request.Context(), email, c.Param("id"), q.Symbol, page, size)
	if err != nil {
		h.transactionError(c, err, "list transactions")
		return
	}
	currentPageCount := len(txs)
	response.OKList(c, txs, response.Meta{
		Total:            &total,
		Page:             &page,
		Size:             &size,
		CurrentPageCount: &currentPageCount,
	})
}

func (h *PortfolioHandler) createTransaction(c *gin.Context) {
	var req createTransactionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Err(c, http.StatusBadRequest, err.Error())
		return
	}
	in := portfoliouc.TransactionInput{
		Symbol:   req.Symbol,
		Name:     req.Name,
		Side:     req.Side,
		Quantity: *req.Quantity,
		Price:    *req.Price,
		Note:     req.Note,
		Date:     req.Date,
	}
	if req.Fee != nil {
		in.Fee = *req.Fee
	}

	email := c.GetString(middleware.EmailKey)
	res, err := h.uc.CreateTransaction(c.Request.Context(), email, c.Param("id"), in)
	if err != nil {
		h.transactionError(c, err, "create transaction", zap.String("symbol", req.Symbol))
		return
	}
	response.Created(c, res)
}

func (h *PortfolioHandler) updateTransaction(c *gin.Context) {
	var req updateTransactionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Err(c, http.StatusBadRequest, err.Error())
		return
	}
	if req.Side == nil && req.Quantity == nil && req.Price == nil && req.Fee == nil && req.Note == nil && req.Date == nil {
		response.Err(c, http.StatusBadRequest, "at least one of side, quantity, price, fee, note or date is required")
		return
	}

	email := c.GetString(middleware.EmailKey)
	res, err := h.uc.UpdateTransaction(c.Request.Context(), email, c.Param("id"), c.Param("txId"), portfoliouc.TransactionPatch{
		Side: req.Side, Quantity: req.Quantity, Price: req.Price, Fee: req.Fee, Note: req.Note, Date: req.Date,
	})
	if err != nil {
		h.transactionError(c, err, "update transaction", zap.String("txId", c.Param("txId")))
		return
	}
	response.OK(c, res)
}

func (h *PortfolioHandler) deleteTransaction(c *gin.Context) {
	email := c.GetString(middleware.EmailKey)
	if err := h.uc.DeleteTransaction(c.Request.Context(), email, c.Param("id"), c.Param("txId")); err != nil {
		h.transactionError(c, err, "delete transaction", zap.String("txId", c.Param("txId")))
		return
	}
	c.Status(http.StatusNoContent)
}
