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
		attributes := []any{
			"request_id", requestID,
			"method", ctx.Request.Method,
			"route", route,
			"status", ctx.Writer.Status(),
			"duration_ms", time.Since(startedAt).Milliseconds(),
		}
		if internalError := ctx.Errors.ByType(gin.ErrorTypePrivate).Last(); internalError != nil {
			attributes = append(attributes, "error", internalError.Err)
			logger.Error("http request completed", attributes...)
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
