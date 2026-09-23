package authentication

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/Rahmannugar/authlier/emailpassword"
	"github.com/Rahmannugar/livdot/internal/infra/ratelimit"
	"github.com/gin-gonic/gin"
)

type ServiceAPI interface {
	Register(ctx context.Context, role Role, credentials Credentials) (Result, error)
	SignIn(ctx context.Context, role Role, credentials Credentials) (Result, error)
	Revoke(ctx context.Context, rawToken string) error
}

type Handler struct {
	service ServiceAPI
}

func NewHandler(service ServiceAPI) *Handler {
	return &Handler{service: service}
}

// auth quotas are per IP: sign-up is tighter than sign-in because it also
// writes an account.
var (
	signUpPolicy = ratelimit.Policy{
		Name: "auth.signup", Burst: 3, RefillPerSecond: 0.2,
		WindowLimit: 30, Window: time.Minute, KeyBy: ratelimit.KeyByIP,
	}
	signInPolicy = ratelimit.Policy{
		Name: "auth.signin", Burst: 5, RefillPerSecond: 0.5,
		WindowLimit: 20, Window: time.Minute, KeyBy: ratelimit.KeyByIP,
	}
	signOutPolicy = ratelimit.Policy{
		Name: "auth.signout", Burst: 10, RefillPerSecond: 2,
		WindowLimit: 60, Window: time.Minute, KeyBy: ratelimit.KeyByIP,
	}
)

func RegisterRoutes(router gin.IRoutes, service ServiceAPI, limiter *ratelimit.Limiter) {
	handler := NewHandler(service)
	signUp := limiter.Middleware(signUpPolicy)
	signIn := limiter.Middleware(signInPolicy)
	router.POST("/api/signup/host", signUp, handler.signupHost)
	router.POST("/api/signin/host", signIn, handler.signin(RoleHost))
	router.POST("/api/signup/crew", signUp, handler.signupCrew)
	router.POST("/api/signin/crew", signIn, handler.signin(RoleCrew))
	router.POST("/api/signup/user", signUp, handler.signupUser)
	router.POST("/api/signin/user", signIn, handler.signin(RoleUser))
	router.POST("/api/signin/internal", signIn, handler.signin(RoleInternalAdmin))
	router.POST("/api/signout", limiter.Middleware(signOutPolicy), handler.signout)
}

// signout revokes the presented token. It is idempotent: an absent or already
// revoked token still returns 204.
func (handler *Handler) signout(ctx *gin.Context) {
	token, ok := bearerToken(ctx)
	if !ok {
		ctx.Status(http.StatusNoContent)
		return
	}
	if err := handler.service.Revoke(ctx.Request.Context(), token); err != nil {
		writeAuthError(ctx, err)
		return
	}
	ctx.Status(http.StatusNoContent)
}

// one request body per auth operation so binding and the generated contract
// describe exactly the fields each endpoint accepts.
type signInRequest struct {
	Email    string `json:"email" binding:"required"`
	Password string `json:"password" binding:"required"`
}

type hostSignUpRequest struct {
	Email    string `json:"email" binding:"required"`
	Password string `json:"password" binding:"required"`
	FullName string `json:"fullName" binding:"required"`
}

type crewSignUpRequest struct {
	Email    string `json:"email" binding:"required"`
	Password string `json:"password" binding:"required"`
	CrewName string `json:"crewName" binding:"required"`
}

type userSignUpRequest struct {
	Email    string `json:"email" binding:"required"`
	Password string `json:"password" binding:"required"`
	FullName string `json:"fullName" binding:"required"`
}

func (handler *Handler) signupHost(ctx *gin.Context) {
	var request hostSignUpRequest
	if err := ctx.ShouldBindJSON(&request); err != nil {
		writeAuthError(ctx, emailpassword.ErrInvalidInput)
		return
	}
	handler.register(ctx, RoleHost, Credentials{
		Email:     request.Email,
		Password:  request.Password,
		FullName:  request.FullName,
		SourceKey: ctx.ClientIP(),
	})
}

func (handler *Handler) signupCrew(ctx *gin.Context) {
	var request crewSignUpRequest
	if err := ctx.ShouldBindJSON(&request); err != nil {
		writeAuthError(ctx, emailpassword.ErrInvalidInput)
		return
	}
	handler.register(ctx, RoleCrew, Credentials{
		Email:     request.Email,
		Password:  request.Password,
		CrewName:  request.CrewName,
		SourceKey: ctx.ClientIP(),
	})
}

func (handler *Handler) signupUser(ctx *gin.Context) {
	var request userSignUpRequest
	if err := ctx.ShouldBindJSON(&request); err != nil {
		writeAuthError(ctx, emailpassword.ErrInvalidInput)
		return
	}
	handler.register(ctx, RoleUser, Credentials{
		Email:     request.Email,
		Password:  request.Password,
		FullName:  request.FullName,
		SourceKey: ctx.ClientIP(),
	})
}

func (handler *Handler) register(ctx *gin.Context, role Role, credentials Credentials) {
	result, err := handler.service.Register(ctx.Request.Context(), role, credentials)
	if err != nil {
		writeAuthError(ctx, err)
		return
	}
	ctx.JSON(http.StatusCreated, result)
}

func (handler *Handler) signin(role Role) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		var request signInRequest
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
