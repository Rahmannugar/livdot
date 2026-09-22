package finance

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/Rahmannugar/livdot/internal/authentication"
	"github.com/Rahmannugar/livdot/internal/infra/ratelimit"
	"github.com/gin-gonic/gin"
)

type ServiceAPI interface {
	RefundEvent(ctx context.Context, eventID string, automatic bool) (int, error)
	Refund(ctx context.Context, id string) (Refund, error)
	Refunds(ctx context.Context, filter RefundFilter) (RefundPage, error)
	Payout(ctx context.Context, id string) (Payout, error)
	Payouts(ctx context.Context, filter PayoutFilter) (PayoutPage, error)
}

type Handler struct {
	service ServiceAPI
}

// finance reads are per account; admin refunds are deliberately tight.
var (
	readPolicy = ratelimit.Policy{
		Name: "finance.read", Burst: 20, RefillPerSecond: 10,
		WindowLimit: 120, Window: time.Minute, KeyBy: ratelimit.KeyByAccount,
	}
	refundPolicy = ratelimit.Policy{
		Name: "finance.refund", Burst: 5, RefillPerSecond: 1,
		WindowLimit: 20, Window: time.Minute, KeyBy: ratelimit.KeyByAccount,
	}
)

func RegisterRoutes(authenticated gin.IRoutes, service ServiceAPI, limiter *ratelimit.Limiter) {
	handler := &Handler{service: service}
	read := limiter.Middleware(readPolicy)
	refund := limiter.Middleware(refundPolicy)
	authenticated.POST("/events/:id/refund",
		authentication.RequireRole(authentication.RoleInternalAdmin), refund, handler.refundEvent)
	authenticated.GET("/refunds",
		authentication.RequireRole(authentication.RoleUser, authentication.RoleInternalAdmin),
		read, handler.listRefunds)
	authenticated.GET("/refunds/:id",
		authentication.RequireRole(authentication.RoleUser, authentication.RoleInternalAdmin),
		read, handler.getRefund)
	authenticated.GET("/payouts",
		authentication.RequireRole(authentication.RoleHost, authentication.RoleInternalAdmin),
		read, handler.listPayouts)
	authenticated.GET("/payouts/:id",
		authentication.RequireRole(authentication.RoleHost, authentication.RoleInternalAdmin),
		read, handler.getPayout)
}

type refundResponse struct {
	ID               string     `json:"id"`
	EventID          string     `json:"eventId"`
	UserID           string     `json:"userId"`
	PurchaseID       string     `json:"purchaseId"`
	AmountMinor      int64      `json:"amountMinor"`
	Status           string     `json:"status"`
	ProviderRefundID *string    `json:"providerRefundId"`
	RefundedAt       *time.Time `json:"refundedAt"`
}

type payoutResponse struct {
	ID               string     `json:"id"`
	EventID          string     `json:"eventId"`
	HostID           string     `json:"hostId"`
	AmountMinor      int64      `json:"amountMinor"`
	Status           string     `json:"status"`
	ProviderPayoutID *string    `json:"providerPayoutId"`
	PaidAt           *time.Time `json:"paidAt"`
}

func (handler *Handler) refundEvent(ctx *gin.Context) {
	count, err := handler.service.RefundEvent(ctx.Request.Context(), ctx.Param("id"), false)
	if err != nil {
		writeFinanceError(ctx, err)
		return
	}
	ctx.JSON(http.StatusAccepted, gin.H{"refunds": count})
}

func (handler *Handler) listRefunds(ctx *gin.Context) {
	identity, ok := authentication.IdentityFrom(ctx)
	if !ok {
		writeFinanceError(ctx, authentication.ErrUnauthenticated)
		return
	}
	filter := RefundFilter{EventID: queryPtr(ctx, "eventId"), Cursor: queryPtr(ctx, "cursor")}
	if identity.Role == authentication.RoleUser {
		// a viewer only ever sees their own refunds.
		filter.UserID = &identity.AccountID
	} else {
		filter.UserID = queryPtr(ctx, "userId")
	}
	pageSize, err := optionalInt32(ctx, "pageSize")
	if err != nil {
		writeFinanceError(ctx, ErrInvalidInput)
		return
	}
	if pageSize != nil {
		filter.PageSize = *pageSize
	}
	page, err := handler.service.Refunds(ctx.Request.Context(), filter)
	if err != nil {
		writeFinanceError(ctx, err)
		return
	}
	response := make([]refundResponse, 0, len(page.Refunds))
	for _, refund := range page.Refunds {
		response = append(response, newRefundResponse(refund))
	}
	ctx.JSON(http.StatusOK, gin.H{"refunds": response, "nextCursor": page.NextCursor})
}

func (handler *Handler) getRefund(ctx *gin.Context) {
	identity, ok := authentication.IdentityFrom(ctx)
	if !ok {
		writeFinanceError(ctx, authentication.ErrUnauthenticated)
		return
	}
	refund, err := handler.service.Refund(ctx.Request.Context(), ctx.Param("id"))
	if err != nil {
		writeFinanceError(ctx, err)
		return
	}
	if identity.Role == authentication.RoleUser && refund.UserID != identity.AccountID {
		writeFinanceError(ctx, ErrForbidden)
		return
	}
	ctx.JSON(http.StatusOK, newRefundResponse(refund))
}

func (handler *Handler) listPayouts(ctx *gin.Context) {
	identity, ok := authentication.IdentityFrom(ctx)
	if !ok {
		writeFinanceError(ctx, authentication.ErrUnauthenticated)
		return
	}
	filter := PayoutFilter{EventID: queryPtr(ctx, "eventId"), Cursor: queryPtr(ctx, "cursor")}
	if identity.Role == authentication.RoleHost {
		// a host only ever sees their own payouts.
		filter.HostID = &identity.AccountID
	} else {
		filter.HostID = queryPtr(ctx, "hostId")
	}
	pageSize, err := optionalInt32(ctx, "pageSize")
	if err != nil {
		writeFinanceError(ctx, ErrInvalidInput)
		return
	}
	if pageSize != nil {
		filter.PageSize = *pageSize
	}
	page, err := handler.service.Payouts(ctx.Request.Context(), filter)
	if err != nil {
		writeFinanceError(ctx, err)
		return
	}
	response := make([]payoutResponse, 0, len(page.Payouts))
	for _, payout := range page.Payouts {
		response = append(response, newPayoutResponse(payout))
	}
	ctx.JSON(http.StatusOK, gin.H{"payouts": response, "nextCursor": page.NextCursor})
}

func (handler *Handler) getPayout(ctx *gin.Context) {
	identity, ok := authentication.IdentityFrom(ctx)
	if !ok {
		writeFinanceError(ctx, authentication.ErrUnauthenticated)
		return
	}
	payout, err := handler.service.Payout(ctx.Request.Context(), ctx.Param("id"))
	if err != nil {
		writeFinanceError(ctx, err)
		return
	}
	if identity.Role == authentication.RoleHost && payout.HostID != identity.AccountID {
		writeFinanceError(ctx, ErrForbidden)
		return
	}
	ctx.JSON(http.StatusOK, newPayoutResponse(payout))
}

func newRefundResponse(refund Refund) refundResponse {
	return refundResponse{
		ID:               refund.ID,
		EventID:          refund.EventID,
		UserID:           refund.UserID,
		PurchaseID:       refund.PurchaseID,
		AmountMinor:      refund.AmountMinor,
		Status:           refund.Status,
		ProviderRefundID: refund.ProviderRefundID,
		RefundedAt:       refund.RefundedAt,
	}
}

func newPayoutResponse(payout Payout) payoutResponse {
	return payoutResponse{
		ID:               payout.ID,
		EventID:          payout.EventID,
		HostID:           payout.HostID,
		AmountMinor:      payout.AmountMinor,
		Status:           payout.Status,
		ProviderPayoutID: payout.ProviderPayoutID,
		PaidAt:           payout.PaidAt,
	}
}

func queryPtr(ctx *gin.Context, key string) *string {
	value := ctx.Query(key)
	if value == "" {
		return nil
	}
	return &value
}

func optionalInt32(ctx *gin.Context, key string) (*int32, error) {
	raw := ctx.Query(key)
	if raw == "" {
		return nil, nil
	}
	value, err := strconv.ParseInt(raw, 10, 32)
	if err != nil {
		return nil, err
	}
	parsed := int32(value)
	return &parsed, nil
}

func writeFinanceError(ctx *gin.Context, err error) {
	status := http.StatusInternalServerError
	code := "finance_unavailable"
	message := "the finance request could not be completed"
	switch {
	case errors.Is(err, authentication.ErrUnauthenticated):
		status = http.StatusUnauthorized
		code = "unauthenticated"
		message = "a valid session token is required"
	case errors.Is(err, ErrInvalidInput):
		status = http.StatusBadRequest
		code = "invalid_request"
		message = "the finance request is invalid"
	case errors.Is(err, ErrNotFound):
		status = http.StatusNotFound
		code = "not_found"
		message = "no refund or payout exists for this identifier"
	case errors.Is(err, ErrForbidden):
		status = http.StatusForbidden
		code = "forbidden"
		message = "this record belongs to another account"
	case errors.Is(err, ErrNotRefundable):
		status = http.StatusConflict
		code = "not_refundable"
		message = "only an event with a failed stream can be refunded"
	}
	ctx.JSON(status, gin.H{"error": gin.H{"code": code, "message": message}})
}
