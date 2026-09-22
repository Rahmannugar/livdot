package webhooks

import (
	"errors"
	"net/http"

	"github.com/Rahmannugar/livdot/internal/infra/payment"
	"github.com/gin-gonic/gin"
)

const signatureHeader = "X-Paystack-Signature"

// RegisterRoutes mounts the provider callback endpoint. It stays public because
// authenticity comes from the provider signature, not a session.
func RegisterRoutes(public gin.IRoutes, service *Service) {
	public.POST("/webhooks/payments", func(ctx *gin.Context) {
		raw, err := ctx.GetRawData()
		if err != nil {
			writeWebhookError(ctx, ErrInvalidInput)
			return
		}
		if err := service.Process(ctx.Request.Context(), raw, ctx.GetHeader(signatureHeader)); err != nil {
			writeWebhookError(ctx, err)
			return
		}
		ctx.JSON(http.StatusOK, gin.H{"status": "received"})
	})
}

func writeWebhookError(ctx *gin.Context, err error) {
	status := http.StatusInternalServerError
	code := "webhook_failed"
	message := "the webhook could not be processed"
	switch {
	case errors.Is(err, ErrInvalidInput):
		status = http.StatusBadRequest
		code = "invalid_webhook"
		message = "the webhook payload is invalid"
	case errors.Is(err, payment.ErrInvalidSignature):
		status = http.StatusUnauthorized
		code = "invalid_signature"
		message = "the webhook signature is invalid"
	case errors.Is(err, payment.ErrInvalidWebhook):
		status = http.StatusBadRequest
		code = "invalid_webhook"
		message = "the webhook payload is invalid"
	}
	ctx.JSON(status, gin.H{"error": gin.H{"code": code, "message": message}})
}
