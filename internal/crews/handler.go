package crews

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/Rahmannugar/livdot/internal/authentication"
	"github.com/Rahmannugar/livdot/internal/infra/ratelimit"
	"github.com/gin-gonic/gin"
)

type ServiceAPI interface {
	Profile(ctx context.Context, accountID string) (Profile, error)
	UpdateProfile(ctx context.Context, accountID string, input UpdateInput) (Profile, error)
	List(ctx context.Context, filter Filter) (Page, error)
}

type Handler struct {
	service ServiceAPI
}

// crew quotas are per account: browsing is cheap, editing is not.
var (
	readPolicy = ratelimit.Policy{
		Name: "crews.read", Burst: 20, RefillPerSecond: 10,
		WindowLimit: 120, Window: time.Minute, KeyBy: ratelimit.KeyByAccount,
	}
	writePolicy = ratelimit.Policy{
		Name: "crews.write", Burst: 10, RefillPerSecond: 2,
		WindowLimit: 30, Window: time.Minute, KeyBy: ratelimit.KeyByAccount,
	}
)

func RegisterRoutes(public gin.IRoutes, authenticated gin.IRoutes, service ServiceAPI, limiter *ratelimit.Limiter) {
	_ = public
	handler := &Handler{service: service}
	read := limiter.Middleware(readPolicy)
	write := limiter.Middleware(writePolicy)
	authenticated.GET("/crews", authentication.RequireRole(authentication.RoleHost), read, handler.list)
	authenticated.GET("/account/crews", authentication.RequireRole(authentication.RoleCrew), read, handler.get)
	authenticated.PATCH("/account/crews", authentication.RequireRole(authentication.RoleCrew), write, handler.update)
}

type profileResponse struct {
	AccountID    string          `json:"accountId"`
	Name         string          `json:"name"`
	List         json.RawMessage `json:"list"`
	Availability string          `json:"availability"`
	UpdatedAt    time.Time       `json:"updatedAt"`
}

type updateProfileRequest struct {
	Name         *string         `json:"name"`
	List         json.RawMessage `json:"list"`
	Availability *string         `json:"availability"`
}

func (handler *Handler) list(ctx *gin.Context) {
	filter := Filter{
		Name:         ctx.Query("name"),
		Availability: Availability(ctx.Query("availability")),
	}
	if raw := ctx.Query("cursor"); raw != "" {
		cursor, err := DecodeCursor(raw)
		if err != nil {
			writeCrewError(ctx, ErrInvalidInput)
			return
		}
		filter.Cursor = cursor
	}
	pageSize, err := optionalInt32(ctx, "pageSize")
	if err != nil {
		writeCrewError(ctx, ErrInvalidInput)
		return
	}
	if pageSize != nil {
		filter.PageSize = *pageSize
	}

	page, err := handler.service.List(ctx.Request.Context(), filter)
	if err != nil {
		writeCrewError(ctx, err)
		return
	}
	response := make([]profileResponse, 0, len(page.Profiles))
	for _, profile := range page.Profiles {
		response = append(response, newProfileResponse(profile))
	}
	ctx.JSON(http.StatusOK, gin.H{"crews": response, "nextCursor": page.NextCursor})
}

func (handler *Handler) get(ctx *gin.Context) {
	identity, ok := authentication.IdentityFrom(ctx)
	if !ok {
		writeCrewError(ctx, authentication.ErrUnauthenticated)
		return
	}
	profile, err := handler.service.Profile(ctx.Request.Context(), identity.AccountID)
	if err != nil {
		writeCrewError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, newProfileResponse(profile))
}

func (handler *Handler) update(ctx *gin.Context) {
	identity, ok := authentication.IdentityFrom(ctx)
	if !ok {
		writeCrewError(ctx, authentication.ErrUnauthenticated)
		return
	}
	var request updateProfileRequest
	if err := ctx.ShouldBindJSON(&request); err != nil {
		writeCrewError(ctx, ErrInvalidInput)
		return
	}

	input := UpdateInput{Name: request.Name, List: request.List}
	if request.Availability != nil {
		availability := Availability(*request.Availability)
		input.Availability = &availability
	}
	profile, err := handler.service.UpdateProfile(ctx.Request.Context(), identity.AccountID, input)
	if err != nil {
		writeCrewError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, newProfileResponse(profile))
}

func newProfileResponse(profile Profile) profileResponse {
	return profileResponse{
		AccountID:    profile.AccountID,
		Name:         profile.Name,
		List:         profile.List,
		Availability: string(profile.Availability),
		UpdatedAt:    profile.UpdatedAt,
	}
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

func writeCrewError(ctx *gin.Context, err error) {
	status := http.StatusInternalServerError
	code := "crews_unavailable"
	message := "the crew request could not be completed"
	switch {
	case errors.Is(err, authentication.ErrUnauthenticated):
		status = http.StatusUnauthorized
		code = "unauthenticated"
		message = "a valid session token is required"
	case errors.Is(err, ErrInvalidInput):
		status = http.StatusBadRequest
		code = "invalid_request"
		message = "crew details are invalid"
	case errors.Is(err, ErrNotFound):
		status = http.StatusNotFound
		code = "crew_not_found"
		message = "no crew exists for this account"
	}
	ctx.JSON(status, gin.H{"error": gin.H{"code": code, "message": message}})
}
