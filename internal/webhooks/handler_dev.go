package webhooks

import (
	"net/http"

	"github.com/Rahmannugar/livdot/internal/infra/payment"
	"github.com/Rahmannugar/livdot/internal/infra/ratelimit"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// RegisterDevSimulator mounts a development-only endpoint that signs and
// delivers a provider webhook as Paystack would. It is never mounted in
// production; it exists so the mocked payment flow is reachable from Postman.
func RegisterDevSimulator(public gin.IRoutes, mock *payment.Mock, service *Service, limiter *ratelimit.Limiter) {
	public.POST("/dev/payments/notify", limiter.Middleware(ratelimit.PolicyWebhook), func(ctx *gin.Context) {
		var request struct {
			PurchaseID string `json:"purchaseId" binding:"required"`
			Outcome    string `json:"outcome" binding:"required"`
		}
		if err := ctx.ShouldBindJSON(&request); err != nil {
			writeWebhookError(ctx, ErrInvalidInput)
			return
		}

		eventType, ok := simulatedEventType(request.Outcome)
		if !ok {
			writeWebhookError(ctx, ErrInvalidInput)
			return
		}
		eventID, err := uuid.NewV7()
		if err != nil {
			writeWebhookError(ctx, err)
			return
		}
		payload, signature := mock.EncodeWebhook(payment.WebhookEvent{
			ID:                eventID.String(),
			Type:              eventType,
			Reference:         request.PurchaseID,
			ProviderReference: "mock_chg_" + request.PurchaseID,
		})
		if err := service.Process(ctx.Request.Context(), payload, signature); err != nil {
			writeWebhookError(ctx, err)
			return
		}
		ctx.JSON(http.StatusOK, gin.H{"status": "delivered", "type": eventType})
	})
}

func simulatedEventType(outcome string) (string, bool) {
	switch outcome {
	case "processing", "initiated":
		return payment.EventChargeProcessing, true
	case "paid":
		return payment.EventChargePaid, true
	case "failed":
		return payment.EventChargeFailed, true
	default:
		return "", false
	}
}
