// Package httpapi owns shared HTTP response and observability behavior.
package httpapi

import (
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

const requestIDContextKey = "livdot.http.request_id"

// probeRoutes are hit by the container healthcheck and by an orchestrator. A
// successful probe is not an outcome worth a log line; a failing one is.
var probeRoutes = map[string]struct{}{
	"/health/live":  {},
	"/health/ready": {},
}

type errorResponse struct {
	Error errorBody `json:"error"`
}

type errorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Field   string `json:"field,omitempty"`
}

// RequestLogger assigns a correlation ID and emits one structured completion
// record per request. It deliberately excludes URLs, query strings, and bodies.
func RequestLogger(logger *slog.Logger) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		requestID := uuid.NewString()
		ctx.Set(requestIDContextKey, requestID)
		ctx.Header("X-Request-ID", requestID)
		startedAt := time.Now()

		ctx.Next()

		route := ctx.FullPath()
		if route == "" {
			route = "unmatched"
		}
		status := ctx.Writer.Status()
		internalError := ctx.Errors.ByType(gin.ErrorTypePrivate).Last()
		// suppress successful probes so they do not bury real traffic; a broken
		// dependency still logs through the error path below.
		if internalError == nil && status < 400 {
			if _, probe := probeRoutes[route]; probe {
				return
			}
		}
		// failed probes have no private error, so promote them to ERROR so an
		// unhealthy dependency is not buried under INFO-level request logs.
		failedProbe := false
		if internalError == nil && status >= 400 {
			if _, probe := probeRoutes[route]; probe {
				failedProbe = true
			}
		}
		attributes := []any{
			"request_id", requestID,
			"method", ctx.Request.Method,
			"route", route,
			"status", status,
			"duration_ms", time.Since(startedAt).Milliseconds(),
		}
		if internalError != nil {
			attributes = append(attributes, "error", internalError.Err)
			logger.Error("http request completed", attributes...)
			return
		}
		if failedProbe {
			logger.Error("health probe failed", attributes...)
			return
		}
		logger.Info("http request completed", attributes...)
	}
}

// Recovery turns panics into a generic response while retaining the cause and
// stack trace in structured server logs.
func Recovery(logger *slog.Logger) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		defer func() {
			if recovered := recover(); recovered != nil {
				logger.Error("http panic recovered",
					"request_id", RequestID(ctx),
					"method", ctx.Request.Method,
					"route", ctx.FullPath(),
					"error", fmt.Sprint(recovered),
					"stack", string(debug.Stack()),
				)
				ctx.Abort()
				if !ctx.Writer.Written() {
					ctx.JSON(http.StatusInternalServerError, errorResponse{Error: errorBody{
						Code: "internal_error", Message: "the request could not be completed",
					}})
				}
			}
		}()
		ctx.Next()
	}
}

// WriteError writes the stable public error shape. Unexpected failures are
// attached privately so RequestLogger records their wrapped internal cause.
func WriteError(ctx *gin.Context, err error, status int, code, message, field string) {
	if status >= http.StatusInternalServerError && err != nil {
		_ = ctx.Error(err).SetType(gin.ErrorTypePrivate)
	}
	ctx.JSON(status, errorResponse{Error: errorBody{
		Code: code, Message: message, Field: field,
	}})
}

// RequestID returns the server-generated request correlation identifier.
func RequestID(ctx *gin.Context) string {
	requestID, _ := ctx.Get(requestIDContextKey)
	value, _ := requestID.(string)
	return value
}
