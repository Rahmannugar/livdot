package httpapi

import (
	"bytes"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestRequestLoggerCorrelatesInternalFailureWithoutLoggingRequestData(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	router := gin.New()
	router.Use(RequestLogger(logger), Recovery(logger))
	router.POST("/events", func(ctx *gin.Context) {
		WriteError(ctx, errors.New("database unavailable"), http.StatusInternalServerError,
			"events_unavailable", "the event request could not be completed", "")
	})

	request := httptest.NewRequest(http.MethodPost, "/events?secret=hidden", strings.NewReader(`{"token":"hidden"}`))
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	requestID := response.Header().Get("X-Request-ID")
	if requestID == "" {
		t.Fatal("X-Request-ID is empty")
	}
	output := logs.String()
	for _, expected := range []string{requestID, `"route":"/events"`, `"status":500`, "database unavailable"} {
		if !strings.Contains(output, expected) {
			t.Fatalf("log output %q does not contain %q", output, expected)
		}
	}
	for _, forbidden := range []string{"secret=hidden", `"token"`, "hidden"} {
		if strings.Contains(output, forbidden) {
			t.Fatalf("log output contains request data %q: %s", forbidden, output)
		}
	}
}
