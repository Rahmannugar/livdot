// Package payment is the payment gateway boundary. Mocked for this build.
package payment

import (
	"context"
	"errors"
)

// one currency for the whole platform for now.
const Currency = "NGN"

// provider name recorded on webhook rows and refs.
const ProviderName = "paystack"

// webhook types we handle. kept provider-neutral so domains don't depend on
// paystack naming.
const (
	EventChargeProcessing = "charge.processing"
	EventChargePaid       = "charge.paid"
	EventChargeFailed     = "charge.failed"
	EventRefundProcessed  = "refund.processed"
	EventRefundFailed     = "refund.failed"
	EventPayoutProcessed  = "payout.processed"
	EventPayoutFailed     = "payout.failed"
)

var (
	ErrInvalidSignature = errors.New("payment webhook signature is invalid")
	ErrInvalidWebhook   = errors.New("payment webhook payload is invalid")
)

// opens a payment intent for a purchase. reference is our purchase id, and the
// idempotency key collapses retries for the same logical payment.
type ChargeRequest struct {
	Reference      string
	IdempotencyKey string
	AmountMinor    int64
	Currency       string
	Email          string
}

type ChargeResult struct {
	ProviderReference string
	CheckoutURL       string
}

// gives money back for a paid purchase.
type RefundRequest struct {
	Reference                string
	IdempotencyKey           string
	AmountMinor              int64
	Currency                 string
	ProviderPaymentReference string
}

type RefundResult struct {
	ProviderReference string
}

// pays a host after the event wraps up.
type PayoutRequest struct {
	Reference         string
	IdempotencyKey    string
	AmountMinor       int64
	Currency          string
	ProviderAccountID string
}

type PayoutResult struct {
	ProviderReference string
}

// verified callback from the provider. reference points back at one of our rows.
type WebhookEvent struct {
	ID                string
	Type              string
	Reference         string
	ProviderReference string
}

// the provider we depend on. implementations use the idempotency key as the
// dedupe key for one money movement.
type Provider interface {
	InitiateCharge(ctx context.Context, request ChargeRequest) (ChargeResult, error)
	InitiateRefund(ctx context.Context, request RefundRequest) (RefundResult, error)
	InitiatePayout(ctx context.Context, request PayoutRequest) (PayoutResult, error)
	VerifyWebhook(payload []byte, signature string) (WebhookEvent, error)
}
