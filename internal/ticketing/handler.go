package ticketing

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/Rahmannugar/livdot/internal/authentication"
	"github.com/Rahmannugar/livdot/internal/infra/httpapi"
	"github.com/Rahmannugar/livdot/internal/infra/ratelimit"
	"github.com/Rahmannugar/livdot/internal/validation"
	"github.com/gin-gonic/gin"
)

type ServiceAPI interface {
	Purchase(ctx context.Context, userID, eventID, idempotencyKey string) (Purchase, error)
	Ticket(ctx context.Context, userID, ticketID string) (Ticket, error)
}

type Handler struct {
	service ServiceAPI
}

// buying holds a slot, so it is the tightest quota; ticket reads are cheap.
var (
	purchasePolicy = ratelimit.Policy{
		Name: "ticketing.purchase", Burst: 3, RefillPerSecond: 0.5,
		WindowLimit: 10, Window: time.Minute, KeyBy: ratelimit.KeyByAccount,
	}
	readPolicy = ratelimit.Policy{
		Name: "ticketing.read", Burst: 20, RefillPerSecond: 10,
		WindowLimit: 120, Window: time.Minute, KeyBy: ratelimit.KeyByAccount,
	}
)

func RegisterRoutes(authenticated gin.IRoutes, service ServiceAPI, limiter *ratelimit.Limiter) {
	handler := &Handler{service: service}
	purchase := limiter.Middleware(purchasePolicy)
	read := limiter.Middleware(readPolicy)
	authenticated.POST("/events/:id/purchase",
		authentication.RequireRole(authentication.RoleUser), purchase, handler.purchase)
	authenticated.GET("/tickets/:id",
		authentication.RequireRole(authentication.RoleUser), read, handler.ticket)
}

type purchaseRequest struct {
	IdempotencyKey string `json:"idempotencyKey" binding:"required"`
}

type purchaseResponse struct {
	ID                string     `json:"id"`
	EventID           string     `json:"eventId"`
	UserID            string     `json:"userId"`
	AmountMinor       int64      `json:"amountMinor"`
	Status            string     `json:"status"`
	CheckoutURL       *string    `json:"checkoutUrl"`
	CheckoutExpiresAt *time.Time `json:"checkoutExpiresAt"`
	TicketID          string     `json:"ticketId"`
	PaidAt            *time.Time `json:"paidAt"`
	CreatedAt         time.Time  `json:"createdAt"`
}

type ticketResponse struct {
	ID                   string     `json:"id"`
	EventID              string     `json:"eventId"`
	UserID               string     `json:"userId"`
	Status               string     `json:"status"`
	ReservationExpiresAt time.Time  `json:"reservationExpiresAt"`
	IssuedAt             *time.Time `json:"issuedAt"`
}

func (handler *Handler) purchase(ctx *gin.Context) {
	identity, ok := authentication.IdentityFrom(ctx)
	if !ok {
		writeTicketError(ctx, authentication.ErrUnauthenticated)
		return
	}
	var request purchaseRequest
	if err := ctx.ShouldBindJSON(&request); err != nil {
		writeTicketError(ctx, validation.New(ErrInvalidInput, "body",
			"request body must include a non-empty idempotencyKey"))
		return
	}
	purchase, err := handler.service.Purchase(
		ctx.Request.Context(), identity.AccountID, ctx.Param("id"), request.IdempotencyKey)
	if err != nil {
		writeTicketError(ctx, err)
		return
	}
	ctx.JSON(http.StatusCreated, newPurchaseResponse(purchase))
}

func (handler *Handler) ticket(ctx *gin.Context) {
	identity, ok := authentication.IdentityFrom(ctx)
	if !ok {
		writeTicketError(ctx, authentication.ErrUnauthenticated)
		return
	}
	ticket, err := handler.service.Ticket(ctx.Request.Context(), identity.AccountID, ctx.Param("id"))
	if err != nil {
		writeTicketError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, newTicketResponse(ticket))
}

func newPurchaseResponse(purchase Purchase) purchaseResponse {
	return purchaseResponse{
		ID:                purchase.ID,
		EventID:           purchase.EventID,
		UserID:            purchase.UserID,
		AmountMinor:       purchase.AmountMinor,
		Status:            purchase.Status,
		CheckoutURL:       purchase.CheckoutURL,
		CheckoutExpiresAt: purchase.CheckoutExpiresAt,
		TicketID:          purchase.TicketID,
		PaidAt:            purchase.PaidAt,
		CreatedAt:         purchase.CreatedAt,
	}
}

func newTicketResponse(ticket Ticket) ticketResponse {
	return ticketResponse{
		ID:                   ticket.ID,
		EventID:              ticket.EventID,
		UserID:               ticket.UserID,
		Status:               ticket.Status,
		ReservationExpiresAt: ticket.ReservationExpiresAt,
		IssuedAt:             ticket.IssuedAt,
	}
}

func writeTicketError(ctx *gin.Context, err error) {
	status := http.StatusInternalServerError
	code := "ticketing_unavailable"
	message := "the ticketing request could not be completed"
	field := ""
	switch {
	case errors.Is(err, authentication.ErrUnauthenticated):
		status = http.StatusUnauthorized
		code = "unauthenticated"
		message = "a valid session token is required"
	case errors.Is(err, ErrInvalidInput):
		status = http.StatusBadRequest
		code = "invalid_request"
		message = "the purchase request is invalid"
		if safeField, safeMessage, ok := validation.Details(err); ok {
			field = safeField
			message = safeMessage
		}
	case errors.Is(err, ErrNotFound):
		status = http.StatusNotFound
		code = "not_found"
		message = "no purchase or ticket exists for this identifier"
	case errors.Is(err, ErrForbidden):
		status = http.StatusForbidden
		code = "forbidden"
		message = "this ticket belongs to another account"
	case errors.Is(err, ErrEventClosed),
		errors.Is(err, ErrSoldOut),
		errors.Is(err, ErrAlreadyPurchased),
		errors.Is(err, ErrReservationGone):
		status = http.StatusConflict
		code = "purchase_conflict"
		message = "the event cannot be purchased in its current state"
	}
	httpapi.WriteError(ctx, err, status, code, message, field)
}
