package authentication

import (
	"context"
	"errors"
	"net/http"

	"github.com/Rahmannugar/authlier/emailpassword"
	"github.com/Rahmannugar/livdot/internal/infra/ratelimit"
	"github.com/gin-gonic/gin"
)

type ServiceAPI interface {
	Register(ctx context.Context, role Role, credentials Credentials) (Result, error)
	SignIn(ctx context.Context, role Role, credentials Credentials) (Result, error)
}

type Handler struct {
	service ServiceAPI
}

func NewHandler(service ServiceAPI) *Handler {
	return &Handler{service: service}
}

func RegisterRoutes(router gin.IRoutes, service ServiceAPI, limiter *ratelimit.Limiter) {
	handler := NewHandler(service)
	guard := limiter.Middleware(ratelimit.PolicyAuth)
	router.POST("/api/signup/host", guard, handler.signup(RoleHost))
	router.POST("/api/signin/host", guard, handler.signin(RoleHost))
	router.POST("/api/signup/crew", guard, handler.signup(RoleCrew))
	router.POST("/api/signin/crew", guard, handler.signin(RoleCrew))
	router.POST("/api/signup/user", guard, handler.signup(RoleUser))
	router.POST("/api/signin/user", guard, handler.signin(RoleUser))
	router.POST("/api/signin/internal", guard, handler.signin(RoleInternalAdmin))
}

type credentialRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	FullName string `json:"fullName"`
	CrewName string `json:"crewName"`
}

func (handler *Handler) signup(role Role) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		var request credentialRequest
		if err := ctx.ShouldBindJSON(&request); err != nil {
			writeAuthError(ctx, emailpassword.ErrInvalidInput)
			return
		}
		result, err := handler.service.Register(ctx.Request.Context(), role, Credentials{
			Email:     request.Email,
			Password:  request.Password,
			FullName:  request.FullName,
			CrewName:  request.CrewName,
			SourceKey: ctx.ClientIP(),
		})
		if err != nil {
			writeAuthError(ctx, err)
			return
		}
		ctx.JSON(http.StatusCreated, result)
	}
}

func (handler *Handler) signin(role Role) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		var request credentialRequest
		if err := ctx.ShouldBindJSON(&request); err != nil {
			writeAuthError(ctx, emailpassword.ErrInvalidInput)
			return
		}
		result, err := handler.service.SignIn(ctx.Request.Context(), role, Credentials{
			Email:     request.Email,
			Password:  request.Password,
			SourceKey: ctx.ClientIP(),
		})
		if err != nil {
			writeAuthError(ctx, err)
			return
		}
		ctx.JSON(http.StatusOK, result)
	}
}

func writeAuthError(ctx *gin.Context, err error) {
	status := http.StatusInternalServerError
	code := "authentication_failed"
	message := "authentication could not be completed"
	switch {
	case errors.Is(err, emailpassword.ErrInvalidInput), errors.Is(err, emailpassword.ErrInvalidPassword):
		status = http.StatusBadRequest
		code = "invalid_request"
		message = "email, password, and profile details are invalid"
	case errors.Is(err, ErrInvalidProfile):
		status = http.StatusBadRequest
		code = "invalid_profile"
		message = "profile details are invalid"
	case errors.Is(err, emailpassword.ErrInvalidCredentials):
		status = http.StatusUnauthorized
		code = "invalid_credentials"
		message = "invalid email or password"
	case errors.Is(err, emailpassword.ErrRegistrationUnavailable):
		status = http.StatusConflict
		code = "registration_unavailable"
		message = "an account already exists for this email"
	case errors.Is(err, ErrRegistrationDisabled):
		status = http.StatusForbidden
		code = "registration_disabled"
		message = "registration is disabled for this role"
	case errors.Is(err, ErrInvalidRole):
		status = http.StatusBadRequest
		code = "invalid_role"
		message = "invalid authentication role"
	}
	ctx.JSON(status, gin.H{"error": gin.H{"code": code, "message": message}})
}
