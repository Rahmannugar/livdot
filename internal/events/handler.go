package events

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/Rahmannugar/livdot/internal/authentication"
	"github.com/Rahmannugar/livdot/internal/infra/httpapi"
	"github.com/Rahmannugar/livdot/internal/infra/ratelimit"
	"github.com/Rahmannugar/livdot/internal/validation"
	"github.com/gin-gonic/gin"
)

type ServiceAPI interface {
	Create(ctx context.Context, hostID string, input CreateInput) (Event, error)
	List(ctx context.Context, filter Filter) (Page, error)
	Detail(ctx context.Context, id string) (Event, error)
	Access(ctx context.Context, eventID, accountID string) (bool, error)
	Update(ctx context.Context, hostID, id string, input UpdateInput) (Event, error)
}

type Handler struct {
	service ServiceAPI
}

// event quotas are per account: reads are generous, writes are guarded.
var (
	readPolicy = ratelimit.Policy{
		Name: "events.read", Burst: 40, RefillPerSecond: 20,
		WindowLimit: 300, Window: time.Minute, KeyBy: ratelimit.KeyByAccount,
	}
	writePolicy = ratelimit.Policy{
		Name: "events.write", Burst: 10, RefillPerSecond: 3,
		WindowLimit: 40, Window: time.Minute, KeyBy: ratelimit.KeyByAccount,
	}
)

func RegisterRoutes(authenticated gin.IRoutes, service ServiceAPI, limiter *ratelimit.Limiter) {
	handler := &Handler{service: service}
	read := limiter.Middleware(readPolicy)
	write := limiter.Middleware(writePolicy)
	authenticated.GET("/events", read, handler.list)
	authenticated.GET("/events/:id", read, handler.detail)
	authenticated.POST("/events", authentication.RequireRole(authentication.RoleHost), write, handler.create)
	authenticated.PATCH("/events/:id", authentication.RequireRole(authentication.RoleHost), write, handler.update)
}

type createEventRequest struct {
	Name            string     `json:"name"`
	AmountMinor     int64      `json:"amountMinor"`
	DurationSeconds int32      `json:"durationSeconds"`
	TotalTickets    int32      `json:"totalTickets"`
	StartsAt        *time.Time `json:"startsAt"`
	AssignedCrewID  *string    `json:"assignedCrewId"`
}

type updateEventRequest struct {
	Name            *string         `json:"name"`
	AmountMinor     *int64          `json:"amountMinor"`
	DurationSeconds *int32          `json:"durationSeconds"`
	TotalTickets    *int32          `json:"totalTickets"`
	StartsAt        *time.Time      `json:"startsAt"`
	AssignedCrewID  json.RawMessage `json:"assignedCrewId"`
	Status          *string         `json:"status"`
}

type crewResponse struct {
	AccountID    string `json:"accountId"`
	Name         string `json:"name"`
	Availability string `json:"availability"`
}

type eventResponse struct {
	ID               string        `json:"id"`
	HostID           string        `json:"hostId"`
	AssignedCrewID   *string       `json:"assignedCrewId"`
	AssignedCrew     *crewResponse `json:"assignedCrew,omitempty"`
	Name             string        `json:"name"`
	AmountMinor      int64         `json:"amountMinor"`
	DurationSeconds  int32         `json:"durationSeconds"`
	Status           string        `json:"status"`
	TotalTickets     int32         `json:"totalTickets"`
	AvailableTickets int32         `json:"availableTickets"`
	StartsAt         time.Time     `json:"startsAt"`
	EndsAt           time.Time     `json:"endsAt"`
	CancelledAt      *time.Time    `json:"cancelledAt"`
	CreatedAt        time.Time     `json:"createdAt"`
	UpdatedAt        time.Time     `json:"updatedAt"`
	Paid             bool          `json:"paid"`
}

func (handler *Handler) create(ctx *gin.Context) {
	identity, ok := authentication.IdentityFrom(ctx)
	if !ok {
		writeEventError(ctx, authentication.ErrUnauthenticated)
		return
	}
	var request createEventRequest
	if err := ctx.ShouldBindJSON(&request); err != nil {
		writeEventError(ctx, validation.New(ErrInvalidInput, "body",
			"request body must be valid JSON with the documented field types"))
		return
	}

	input := CreateInput{
		Name:            request.Name,
		AmountMinor:     request.AmountMinor,
		DurationSeconds: request.DurationSeconds,
		TotalTickets:    request.TotalTickets,
		AssignedCrewID:  request.AssignedCrewID,
	}
	if request.StartsAt != nil {
		input.StartsAt = *request.StartsAt
	}

	event, err := handler.service.Create(ctx.Request.Context(), identity.AccountID, input)
	if err != nil {
		writeEventError(ctx, err)
		return
	}
	ctx.JSON(http.StatusCreated, newEventResponse(event))
}

func (handler *Handler) list(ctx *gin.Context) {
	filter, err := parseEventFilter(ctx)
	if err != nil {
		writeEventError(ctx, err)
		return
	}
	page, err := handler.service.List(ctx.Request.Context(), filter)
	if err != nil {
		writeEventError(ctx, err)
		return
	}
	response := make([]eventResponse, 0, len(page.Events))
	for _, event := range page.Events {
		response = append(response, newEventResponse(event))
	}
	ctx.JSON(http.StatusOK, gin.H{"events": response, "nextCursor": page.NextCursor})
}

func (handler *Handler) detail(ctx *gin.Context) {
	identity, ok := authentication.IdentityFrom(ctx)
	if !ok {
		writeEventError(ctx, authentication.ErrUnauthenticated)
		return
	}
	event, err := handler.service.Detail(ctx.Request.Context(), ctx.Param("id"))
	if err != nil {
		writeEventError(ctx, err)
		return
	}
	response := newEventResponse(event)
	// the caller's own paid access, not the event's.
	if paid, err := handler.service.Access(ctx.Request.Context(), event.ID, identity.AccountID); err == nil {
		response.Paid = paid
	}
	ctx.JSON(http.StatusOK, response)
}

func (handler *Handler) update(ctx *gin.Context) {
	identity, ok := authentication.IdentityFrom(ctx)
	if !ok {
		writeEventError(ctx, authentication.ErrUnauthenticated)
		return
	}
	var request updateEventRequest
	if err := ctx.ShouldBindJSON(&request); err != nil {
		writeEventError(ctx, validation.New(ErrInvalidInput, "body",
			"request body must be valid JSON with the documented field types"))
		return
	}

	input := UpdateInput{
		Name:            request.Name,
		AmountMinor:     request.AmountMinor,
		DurationSeconds: request.DurationSeconds,
		TotalTickets:    request.TotalTickets,
		StartsAt:        request.StartsAt,
	}
	if request.Status != nil {
		if *request.Status != string(StatusCancelled) {
			writeEventError(ctx, validation.New(ErrInvalidInput, "status",
				"status may only be set to cancelled"))
			return
		}
		input.Cancel = true
	}
	if len(request.AssignedCrewID) > 0 {
		input.AssignedCrewSet = true
		if trimmed := bytes.TrimSpace(request.AssignedCrewID); string(trimmed) != "null" {
			var crewID string
			if err := json.Unmarshal(trimmed, &crewID); err != nil || crewID == "" {
				writeEventError(ctx, validation.New(ErrInvalidInput, "assignedCrewId",
					"assignedCrewId must be a non-empty UUID or null"))
				return
			}
			input.AssignedCrewID = &crewID
		}
	}

	event, err := handler.service.Update(ctx.Request.Context(), identity.AccountID, ctx.Param("id"), input)
	if err != nil {
		writeEventError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, newEventResponse(event))
}

func parseEventFilter(ctx *gin.Context) (Filter, error) {
	var filter Filter
	if name := ctx.Query("name"); name != "" {
		filter.Name = &name
	}
	if status := ctx.Query("status"); status != "" {
		parsed := Status(status)
		filter.Status = &parsed
	}
	durationGte, err := optionalInt32(ctx, "duration[gte]")
	if err != nil {
		return Filter{}, err
	}
	durationLte, err := optionalInt32(ctx, "duration[lte]")
	if err != nil {
		return Filter{}, err
	}
	amountGte, err := optionalInt64(ctx, "amount[gte]")
	if err != nil {
		return Filter{}, err
	}
	amountLte, err := optionalInt64(ctx, "amount[lte]")
	if err != nil {
		return Filter{}, err
	}
	pageSize, err := optionalInt32(ctx, "pageSize")
	if err != nil {
		return Filter{}, err
	}
	if raw := ctx.Query("cursor"); raw != "" {
		cursor, err := DecodeCursor(raw)
		if err != nil {
			return Filter{}, err
		}
		filter.Cursor = cursor
	}

	filter.DurationGte = durationGte
	filter.DurationLte = durationLte
	filter.AmountGte = amountGte
	filter.AmountLte = amountLte
	if pageSize != nil {
		filter.PageSize = *pageSize
	}
	return filter, nil
}

func newEventResponse(event Event) eventResponse {
	response := eventResponse{
		ID:               event.ID,
		HostID:           event.HostID,
		AssignedCrewID:   event.AssignedCrewID,
		Name:             event.Name,
		AmountMinor:      event.AmountMinor,
		DurationSeconds:  event.DurationSeconds,
		Status:           string(event.Status),
		TotalTickets:     event.TotalTickets,
		AvailableTickets: event.AvailableTickets,
		StartsAt:         event.StartsAt,
		EndsAt:           event.EndsAt,
		CancelledAt:      event.CancelledAt,
		CreatedAt:        event.CreatedAt,
		UpdatedAt:        event.UpdatedAt,
	}
	if event.AssignedCrew != nil {
		response.AssignedCrew = &crewResponse{
			AccountID:    event.AssignedCrew.AccountID,
			Name:         event.AssignedCrew.Name,
			Availability: event.AssignedCrew.Availability,
		}
	}
	return response
}

func optionalInt32(ctx *gin.Context, key string) (*int32, error) {
	raw := ctx.Query(key)
	if raw == "" {
		return nil, nil
	}
	value, err := strconv.ParseInt(raw, 10, 32)
	if err != nil {
		return nil, validation.New(ErrInvalidInput, key, key+" must be a valid integer")
	}
	parsed := int32(value)
	return &parsed, nil
}

func optionalInt64(ctx *gin.Context, key string) (*int64, error) {
	raw := ctx.Query(key)
	if raw == "" {
		return nil, nil
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return nil, validation.New(ErrInvalidInput, key, key+" must be a valid integer")
	}
	return &value, nil
}

func writeEventError(ctx *gin.Context, err error) {
	status := http.StatusInternalServerError
	code := "events_unavailable"
	message := "the event request could not be completed"
	field := ""
	switch {
	case errors.Is(err, authentication.ErrUnauthenticated):
		status = http.StatusUnauthorized
		code = "unauthenticated"
		message = "a valid session token is required"
	case errors.Is(err, ErrInvalidInput):
		status = http.StatusBadRequest
		code = "invalid_request"
		message = "event details are invalid"
		if safeField, safeMessage, ok := validation.Details(err); ok {
			field = safeField
			message = safeMessage
		}
	case errors.Is(err, ErrNotFound):
		status = http.StatusNotFound
		code = "event_not_found"
		message = "no event exists for this identifier"
	case errors.Is(err, ErrForbidden):
		status = http.StatusForbidden
		code = "forbidden"
		message = "this host does not own the event"
	case errors.Is(err, ErrNotUpdatable),
		errors.Is(err, ErrNotCancellable),
		errors.Is(err, ErrTicketCountConflict),
		errors.Is(err, ErrConflict):
		status = http.StatusConflict
		code = "event_conflict"
		message = "the event cannot be changed in its current state"
	}
	httpapi.WriteError(ctx, err, status, code, message, field)
}
