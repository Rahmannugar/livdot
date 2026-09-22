package payment

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
)

// mocked paystack. no network calls, so refs and signatures are stable.
type Mock struct {
	secret  []byte
	baseURL string
}

// an empty secret is a config error, not a silent unsigned mode.
func NewMock(secret, baseURL string) (*Mock, error) {
	if strings.TrimSpace(secret) == "" {
		return nil, fmt.Errorf("payment webhook secret is required")
	}
	if strings.TrimSpace(baseURL) == "" {
		return nil, fmt.Errorf("payment base URL is required")
	}
	return &Mock{secret: []byte(secret), baseURL: strings.TrimRight(baseURL, "/")}, nil
}

func (mock *Mock) InitiateCharge(_ context.Context, request ChargeRequest) (ChargeResult, error) {
	if err := validateReference(request.Reference, request.IdempotencyKey); err != nil {
		return ChargeResult{}, err
	}
	slog.Info("mock payment charge initiated",
		"component", "payment",
		"reference", request.Reference,
		"amount_minor", request.AmountMinor,
	)
	return ChargeResult{
		ProviderReference: "mock_chg_" + request.Reference,
		CheckoutURL:       mock.baseURL + "/checkout/" + request.Reference,
	}, nil
}

func (mock *Mock) InitiateRefund(_ context.Context, request RefundRequest) (RefundResult, error) {
	if err := validateReference(request.Reference, request.IdempotencyKey); err != nil {
		return RefundResult{}, err
	}
	slog.Info("mock payment refund initiated",
		"component", "payment",
		"reference", request.Reference,
		"amount_minor", request.AmountMinor,
	)
	return RefundResult{ProviderReference: "mock_rfnd_" + request.Reference}, nil
}

func (mock *Mock) InitiatePayout(_ context.Context, request PayoutRequest) (PayoutResult, error) {
	if err := validateReference(request.Reference, request.IdempotencyKey); err != nil {
		return PayoutResult{}, err
	}
	slog.Info("mock payment payout initiated",
		"component", "payment",
		"reference", request.Reference,
		"amount_minor", request.AmountMinor,
	)
	return PayoutResult{ProviderReference: "mock_pyt_" + request.Reference}, nil
}

type webhookPayload struct {
	ID                string `json:"id"`
	Type              string `json:"type"`
	Reference         string `json:"reference"`
	ProviderReference string `json:"providerReference"`
}

func (mock *Mock) VerifyWebhook(payload []byte, signature string) (WebhookEvent, error) {
	// constant time so bad signatures don't leak timing.
	if !hmac.Equal([]byte(mock.Sign(payload)), []byte(strings.TrimSpace(signature))) {
		return WebhookEvent{}, ErrInvalidSignature
	}
	var body webhookPayload
	if err := json.Unmarshal(payload, &body); err != nil {
		return WebhookEvent{}, ErrInvalidWebhook
	}
	if strings.TrimSpace(body.ID) == "" || strings.TrimSpace(body.Type) == "" ||
		strings.TrimSpace(body.Reference) == "" {
		return WebhookEvent{}, ErrInvalidWebhook
	}
	slog.Info("mock payment webhook verified",
		"component", "payment",
		"webhook_id", body.ID,
		"type", body.Type,
		"reference", body.Reference,
	)
	return WebhookEvent{
		ID:                body.ID,
		Type:              body.Type,
		Reference:         body.Reference,
		ProviderReference: body.ProviderReference,
	}, nil
}

// the signature a caller must send in the webhook signature header. exists so
// tests and the dev simulator can produce a valid callback.
func (mock *Mock) Sign(payload []byte) string {
	mac := hmac.New(sha256.New, mock.secret)
	mac.Write(payload)
	return hex.EncodeToString(mac.Sum(nil))
}

// builds a signed payload for the dev-only simulator.
func (mock *Mock) EncodeWebhook(event WebhookEvent) (payload []byte, signature string) {
	body, err := json.Marshal(webhookPayload{
		ID:                event.ID,
		Type:              event.Type,
		Reference:         event.Reference,
		ProviderReference: event.ProviderReference,
	})
	if err != nil {
		return nil, ""
	}
	return body, mock.Sign(body)
}

func validateReference(reference, idempotencyKey string) error {
	if strings.TrimSpace(reference) == "" {
		return fmt.Errorf("payment reference is required")
	}
	if strings.TrimSpace(idempotencyKey) == "" {
		return fmt.Errorf("payment idempotency key is required")
	}
	return nil
}
