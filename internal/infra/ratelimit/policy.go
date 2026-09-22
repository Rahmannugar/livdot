package ratelimit

import "time"

// KeyBy selects what the limit is counted against.
type KeyBy string

const (
	KeyByIP      KeyBy = "ip"
	KeyByAccount KeyBy = "account"
)

// Policy is one route's quota. The token bucket allows a short burst; the
// sliding window caps the total within the window.
type Policy struct {
	Name            string
	Burst           int
	RefillPerSecond float64
	WindowLimit     int
	Window          time.Duration
	KeyBy           KeyBy
}

// per-route quotas. Sensitive or expensive routes are tighter than reads.
var (
	PolicyAuth = Policy{
		Name: "auth", Burst: 5, RefillPerSecond: 0.5,
		WindowLimit: 20, Window: time.Minute, KeyBy: KeyByIP,
	}
	PolicyWebhook = Policy{
		Name: "webhook", Burst: 20, RefillPerSecond: 5,
		WindowLimit: 120, Window: time.Minute, KeyBy: KeyByIP,
	}
	PolicyPurchase = Policy{
		Name: "purchase", Burst: 5, RefillPerSecond: 1,
		WindowLimit: 20, Window: time.Minute, KeyBy: KeyByAccount,
	}
	PolicyWrite = Policy{
		Name: "write", Burst: 15, RefillPerSecond: 5,
		WindowLimit: 60, Window: time.Minute, KeyBy: KeyByAccount,
	}
	PolicyRead = Policy{
		Name: "read", Burst: 40, RefillPerSecond: 20,
		WindowLimit: 300, Window: time.Minute, KeyBy: KeyByAccount,
	}
)
