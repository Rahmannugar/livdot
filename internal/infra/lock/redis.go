// Package lock provides a Redis-backed mutual exclusion lock.
package lock

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

// the lock is held by someone else.
var ErrNotAcquired = errors.New("lock is held by another owner")

// refreshing and releasing must only touch a lock we still own, so both are
// compare-and-act scripts keyed on the owner token.
var (
	refreshScript = redis.NewScript(`
if redis.call("get", KEYS[1]) == ARGV[1] then
  return redis.call("pexpire", KEYS[1], ARGV[2])
end
return 0
`)
	releaseScript = redis.NewScript(`
if redis.call("get", KEYS[1]) == ARGV[1] then
  return redis.call("del", KEYS[1])
end
return 0
`)
)

// Lock is a single-owner lock with a TTL. The owner must refresh before the TTL
// lapses or the lock is released automatically.
type Lock struct {
	client *redis.Client
	key    string
	ttl    time.Duration
	owner  string
}

func New(client *redis.Client, key string, ttl time.Duration) (*Lock, error) {
	if client == nil {
		return nil, fmt.Errorf("lock redis client is required")
	}
	if key == "" {
		return nil, fmt.Errorf("lock key is required")
	}
	if ttl <= 0 {
		return nil, fmt.Errorf("lock TTL must be positive")
	}
	owner, err := uuid.NewV7()
	if err != nil {
		return nil, fmt.Errorf("generate lock owner: %w", err)
	}
	return &Lock{client: client, key: key, ttl: ttl, owner: owner.String()}, nil
}

// Acquire takes the lock if it is free.
func (lock *Lock) Acquire(ctx context.Context) error {
	acquired, err := lock.client.SetNX(ctx, lock.key, lock.owner, lock.ttl).Result()
	if err != nil {
		return fmt.Errorf("acquire lock: %w", err)
	}
	if !acquired {
		return ErrNotAcquired
	}
	return nil
}

// Refresh extends the TTL, but only while we still own the lock.
func (lock *Lock) Refresh(ctx context.Context) error {
	result, err := refreshScript.Run(ctx, lock.client, []string{lock.key}, lock.owner, lock.ttl.Milliseconds()).Int()
	if err != nil {
		return fmt.Errorf("refresh lock: %w", err)
	}
	if result == 0 {
		return ErrNotAcquired
	}
	return nil
}

// Release drops the lock, but only while we still own it.
func (lock *Lock) Release(ctx context.Context) error {
	if _, err := releaseScript.Run(ctx, lock.client, []string{lock.key}, lock.owner).Result(); err != nil {
		return fmt.Errorf("release lock: %w", err)
	}
	return nil
}
