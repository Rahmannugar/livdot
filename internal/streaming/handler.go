package streaming

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
	Start(ctx context.Context, hostID, eventID string) (Stream, error)
	Join(ctx context.Context, userID, eventID string) (Session, error)
	End(ctx context.Context, hostID, eventID string) (Stream, error)
	Fail(ctx context.Context, hostID, eventID, reason string) (Stream, error)
}

type Handler struct {
	service ServiceAPI
}

// stream control is rare; joins are frequent while a stream is live.
var (
	controlPolicy = ratelimit.Policy{
		Name: "streaming.control", Burst: 5, RefillPerSecond: 1,
		WindowLimit: 20, Window: time.Minute, KeyBy: ratelimit.KeyByAccount,
	}
	joinPolicy = ratelimit.Policy{
		Name: "streaming.join", Burst: 5, RefillPerSecond: 1,
		WindowLimit: 30, Window: time.Minute, KeyBy: ratelimit.KeyByAccount,
	}
)

func RegisterRoutes(authenticated gin.IRoutes, service ServiceAPI, limiter *ratelimit.Limiter) {
	handler := &Handler{service: service}
	control := limiter.Middleware(controlPolicy)
	join := limiter.Middleware(joinPolicy)
	authenticated.POST("/events/:id/start",
		authentication.RequireRole(authentication.RoleHost), control, handler.start)
	authenticated.POST("/events/:id/end",
		authentication.RequireRole(authentication.RoleHost), control, handler.end)
	authenticated.POST("/events/:id/stream-failure",
		authentication.RequireRole(authentication.RoleHost), control, handler.fail)
	authenticated.POST("/events/:id/join",
		authentication.RequireRole(authentication.RoleUser), join, handler.join)
}

type streamResponse struct {
	ID            string     `json:"id"`
	EventID       string     `json:"eventId"`
	RoomID        string     `json:"roomId"`
	Status        string     `json:"status"`
	StartedAt     time.Time  `json:"startedAt"`
	FinishedAt    *time.Time `json:"finishedAt"`
	FailedAt      *time.Time `json:"failedAt"`
	FailureReason *string    `json:"failureReason"`
}

type sessionResponse struct {
	Token     string    `json:"token"`
	Room      string    `json:"room"`
	URL       string    `json:"url"`
	ExpiresAt time.Time `json:"expiresAt"`
}

type failureRequest struct {
	Reason string `json:"reason" binding:"required"`
}

func (handler *Handler) start(ctx *gin.Context) {
	identity, ok := authentication.IdentityFrom(ctx)
	if !ok {
		writeStreamError(ctx, authentication.ErrUnauthenticated)
		return
	}
	stream, err := handler.service.Start(ctx.Request.Context(), identity.AccountID, ctx.Param("id"))
	if err != nil {
		writeStreamError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, newStreamResponse(stream))
}

func (handler *Handler) end(ctx *gin.Context) {
	identity, ok := authentication.IdentityFrom(ctx)
	if !ok {
		writeStreamError(ctx, authentication.ErrUnauthenticated)
		return
	}
	stream, err := handler.service.End(ctx.Request.Context(), identity.AccountID, ctx.Param("id"))
	if err != nil {
		writeStreamError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, newStreamResponse(stream))
}

func (handler *Handler) fail(ctx *gin.Context) {
	identity, ok := authentication.IdentityFrom(ctx)
	if !ok {
		writeStreamError(ctx, authentication.ErrUnauthenticated)
		return
	}
	var request failureRequest
	if err := ctx.ShouldBindJSON(&request); err != nil {
		writeStreamError(ctx, validation.New(ErrInvalidInput, "reason", "reason is required"))
		return
	}
	stream, err := handler.service.Fail(
		ctx.Request.Context(), identity.AccountID, ctx.Param("id"), request.Reason)
	if err != nil {
		writeStreamError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, newStreamResponse(stream))
}

func (handler *Handler) join(ctx *gin.Context) {
	identity, ok := authentication.IdentityFrom(ctx)
	if !ok {
		writeStreamError(ctx, authentication.ErrUnauthenticated)
		return
	}
	session, err := handler.service.Join(ctx.Request.Context(), identity.AccountID, ctx.Param("id"))
	if err != nil {
		writeStreamError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, sessionResponse{
		Token:     session.Token,
		Room:      session.Room,
		URL:       session.URL,
		ExpiresAt: session.ExpiresAt,
	})
}

func newStreamResponse(stream Stream) streamResponse {
	return streamResponse{
		ID:            stream.ID,
		EventID:       stream.EventID,
		RoomID:        stream.RoomID,
		Status:        stream.Status,
		StartedAt:     stream.StartedAt,
		FinishedAt:    stream.FinishedAt,
		FailedAt:      stream.FailedAt,
		FailureReason: stream.FailureReason,
	}
}

func writeStreamError(ctx *gin.Context, err error) {
	status := http.StatusInternalServerError
	code := "streaming_unavailable"
	message := "the streaming request could not be completed"
	field := ""
	switch {
	case errors.Is(err, authentication.ErrUnauthenticated):
		status = http.StatusUnauthorized
		code = "unauthenticated"
		message = "a valid session token is required"
	case errors.Is(err, ErrInvalidInput):
		status = http.StatusBadRequest
		code = "invalid_request"
		message = "the streaming request is invalid"
		if safeField, safeMessage, ok := validation.Details(err); ok {
			field = safeField
			message = safeMessage
		}
	case errors.Is(err, ErrNotFound):
		status = http.StatusNotFound
		code = "not_found"
		message = "no event or stream exists for this identifier"
	case errors.Is(err, ErrForbidden):
		status = http.StatusForbidden
		code = "forbidden"
		message = "this host does not own the event"
	case errors.Is(err, ErrNotMember):
		status = http.StatusForbidden
		code = "stream_access_denied"
		message = "only ticket holders can join this stream"
	case errors.Is(err, ErrNotStartable), errors.Is(err, ErrNotLive):
		status = http.StatusConflict
		code = "stream_conflict"
		message = "the stream cannot change in the event's current state"
	}
	httpapi.WriteError(ctx, err, status, code, message, field)
}
