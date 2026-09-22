package ratelimit

import "time"

// KeyBy selects what a limit is counted against. Domains choose per route.
type KeyBy string

const (
	KeyByIP      KeyBy = "ip"
	KeyByAccount KeyBy = "account"
)

// Policy is one route's quota: a token bucket that allows a short burst plus a
// sliding window that caps the total across the window. Each domain defines its
// own policies next to its routes.
type Policy struct {
	Name            string
	Burst           int
	RefillPerSecond float64
	WindowLimit     int
	Window          time.Duration
	KeyBy           KeyBy
}
