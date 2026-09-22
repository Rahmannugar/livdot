package payment

import "testing"

func TestVerifyWebhookRoundTrip(t *testing.T) {
	mock, err := NewMock("secret", "https://mock.paystack.local")
	if err != nil {
		t.Fatalf("NewMock() error = %v", err)
	}
	want := WebhookEvent{ID: "evt_1", Type: EventChargePaid, Reference: "purchase-1", ProviderReference: "mock_chg_purchase-1"}

	payload, signature := mock.EncodeWebhook(want)
	got, err := mock.VerifyWebhook(payload, signature)
	if err != nil {
		t.Fatalf("VerifyWebhook() error = %v", err)
	}
	if got != want {
		t.Fatalf("VerifyWebhook() = %+v, want %+v", got, want)
	}
}

func TestVerifyWebhookRejectsBadSignature(t *testing.T) {
	mock, err := NewMock("secret", "https://mock.paystack.local")
	if err != nil {
		t.Fatalf("NewMock() error = %v", err)
	}
	payload, _ := mock.EncodeWebhook(WebhookEvent{ID: "evt_1", Type: EventChargePaid, Reference: "purchase-1"})

	if _, err := mock.VerifyWebhook(payload, "forged"); err != ErrInvalidSignature {
		t.Fatalf("VerifyWebhook() error = %v, want ErrInvalidSignature", err)
	}
}

func TestNewMockRequiresSecret(t *testing.T) {
	if _, err := NewMock("", "https://mock.paystack.local"); err == nil {
		t.Fatal("NewMock() with empty secret error = nil, want error")
	}
}
