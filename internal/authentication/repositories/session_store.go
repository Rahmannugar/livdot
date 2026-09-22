package authenticationrepo

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/Rahmannugar/authlier/sessiontoken"
	authenticationdb "github.com/Rahmannugar/livdot/internal/authentication/repositories/generated"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

type SessionStore struct {
	pool *pgxpool.Pool
}

func NewSessionStore(pool *pgxpool.Pool) *SessionStore {
	return &SessionStore{pool: pool}
}

func (store *SessionStore) Create(ctx context.Context, record sessiontoken.Record) error {
	err := authenticationdb.New(store.pool).CreateSession(ctx, authenticationdb.CreateSessionParams{
		TokenHash:  record.TokenHash[:],
		ID:         mustUUID(record.ID),
		SubjectID:  mustUUID(record.SubjectID),
		CreatedAt:  timestamp(record.CreatedAt),
		ExpiresAt:  timestamp(record.ExpiresAt),
		ExtendedAt: nullableTimestamp(record.ExtendedAt),
		RevokedAt:  nullableTimestamp(record.RevokedAt),
	})
	if isUniqueViolation(err) {
		return sessiontoken.ErrConflict
	}
	return err
}

func (store *SessionStore) FindByTokenHash(
	ctx context.Context,
	tokenHash sessiontoken.TokenHash,
) (sessiontoken.Record, error) {
	session, err := authenticationdb.New(store.pool).FindSessionByTokenHash(ctx, tokenHash[:])
	if errors.Is(err, pgx.ErrNoRows) {
		return sessiontoken.Record{}, sessiontoken.ErrNotFound
	}
	if err != nil {
		return sessiontoken.Record{}, err
	}
	return sessionRecord(session)
}

func (store *SessionStore) ListBySubject(
	ctx context.Context,
	subjectID string,
) ([]sessiontoken.Record, error) {
	sessions, err := authenticationdb.New(store.pool).ListSessionsBySubject(ctx, mustUUID(subjectID))
	if err != nil {
		return nil, err
	}
	records := make([]sessiontoken.Record, 0, len(sessions))
	for _, session := range sessions {
		record, err := sessionRecord(session)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, nil
}

func (store *SessionStore) Extend(
	ctx context.Context,
	tokenHash sessiontoken.TokenHash,
	extendedAt time.Time,
	expiresAt time.Time,
) (sessiontoken.Record, error) {
	session, err := authenticationdb.New(store.pool).ExtendSession(ctx, authenticationdb.ExtendSessionParams{
		TokenHash:  tokenHash[:],
		ExtendedAt: timestamp(extendedAt),
		ExpiresAt:  timestamp(expiresAt),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return sessiontoken.Record{}, sessiontoken.ErrInactive
	}
	if err != nil {
		return sessiontoken.Record{}, err
	}
	return sessionRecord(session)
}

func (store *SessionStore) Rotate(
	ctx context.Context,
	current sessiontoken.TokenHash,
	replacement sessiontoken.Record,
	rotatedAt time.Time,
) error {
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var expiresAt time.Time
	var revokedAt *time.Time
	var subjectID uuid.UUID
	err = tx.QueryRow(ctx, `
		SELECT subject_id, expires_at, revoked_at
		FROM authentication_sessions
		WHERE token_hash = $1
		FOR UPDATE`, current[:]).Scan(&subjectID, &expiresAt, &revokedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return sessiontoken.ErrNotFound
	}
	if err != nil {
		return err
	}
	if revokedAt != nil || !rotatedAt.Before(expiresAt) || subjectID != mustUUID(replacement.SubjectID) {
		return sessiontoken.ErrInactive
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO authentication_sessions
		    (token_hash, id, subject_id, created_at, expires_at, extended_at, revoked_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		replacement.TokenHash[:],
		mustUUID(replacement.ID),
		mustUUID(replacement.SubjectID),
		replacement.CreatedAt,
		replacement.ExpiresAt,
		replacement.ExtendedAt,
		replacement.RevokedAt,
	); err != nil {
		if isUniqueViolation(err) {
			return sessiontoken.ErrConflict
		}
		return err
	}
	if _, err := tx.Exec(ctx,
		"UPDATE authentication_sessions SET revoked_at = $1 WHERE token_hash = $2",
		rotatedAt,
		current[:],
	); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (store *SessionStore) Revoke(
	ctx context.Context,
	tokenHash sessiontoken.TokenHash,
	revokedAt time.Time,
) error {
	rows, err := authenticationdb.New(store.pool).RevokeSession(ctx, authenticationdb.RevokeSessionParams{
		TokenHash: tokenHash[:],
		RevokedAt: timestamp(revokedAt),
	})
	if err != nil {
		return err
	}
	if rows == 0 {
		return sessiontoken.ErrNotFound
	}
	return nil
}

func (store *SessionStore) RevokeAll(
	ctx context.Context,
	subjectID string,
	revokedAt time.Time,
) ([]sessiontoken.Record, error) {
	sessions, err := authenticationdb.New(store.pool).RevokeSessionsBySubject(ctx, authenticationdb.RevokeSessionsBySubjectParams{
		SubjectID: mustUUID(subjectID),
		RevokedAt: timestamp(revokedAt),
	})
	if err != nil {
		return nil, err
	}
	records := make([]sessiontoken.Record, 0, len(sessions))
	for _, session := range sessions {
		record, err := sessionRecord(session)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, nil
}

type SessionCache struct {
	client *redis.Client
	prefix string
}

func NewSessionCache(client *redis.Client) *SessionCache {
	return &SessionCache{client: client, prefix: "livdot:auth:session:"}
}

func (cache *SessionCache) Get(ctx context.Context, tokenHash sessiontoken.TokenHash) (sessiontoken.Record, error) {
	encoded, err := cache.client.Get(ctx, cache.key(tokenHash)).Bytes()
	if errors.Is(err, redis.Nil) {
		return sessiontoken.Record{}, sessiontoken.ErrCacheMiss
	}
	if err != nil {
		return sessiontoken.Record{}, err
	}
	var record sessiontoken.Record
	if err := json.Unmarshal(encoded, &record); err != nil {
		return sessiontoken.Record{}, fmt.Errorf("decode cached session: %w", err)
	}
	return record, nil
}

func (cache *SessionCache) Set(
	ctx context.Context,
	record sessiontoken.Record,
	ttl time.Duration,
) error {
	encoded, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("encode cached session: %w", err)
	}
	return cache.client.Set(ctx, cache.key(record.TokenHash), encoded, ttl).Err()
}

func (cache *SessionCache) Delete(ctx context.Context, tokenHash sessiontoken.TokenHash) error {
	return cache.client.Del(ctx, cache.key(tokenHash)).Err()
}

func (cache *SessionCache) key(tokenHash sessiontoken.TokenHash) string {
	return cache.prefix + hex.EncodeToString(tokenHash[:])
}

func sessionRecord(session authenticationdb.AuthenticationSession) (sessiontoken.Record, error) {
	if len(session.TokenHash) != 32 {
		return sessiontoken.Record{}, fmt.Errorf("invalid stored session token hash length: %d", len(session.TokenHash))
	}
	var tokenHash sessiontoken.TokenHash
	copy(tokenHash[:], session.TokenHash)
	return sessiontoken.Record{
		ID:         session.ID.String(),
		SubjectID:  session.SubjectID.String(),
		TokenHash:  tokenHash,
		CreatedAt:  session.CreatedAt.Time,
		ExpiresAt:  session.ExpiresAt.Time,
		ExtendedAt: nullableTime(session.ExtendedAt),
		RevokedAt:  nullableTime(session.RevokedAt),
	}, nil
}

func timestamp(value time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: value, Valid: true}
}

func nullableTimestamp(value *time.Time) pgtype.Timestamptz {
	if value == nil {
		return pgtype.Timestamptz{}
	}
	return timestamp(*value)
}

func nullableTime(value pgtype.Timestamptz) *time.Time {
	if !value.Valid {
		return nil
	}
	result := value.Time
	return &result
}

func mustUUID(raw string) uuid.UUID {
	value, err := uuid.Parse(raw)
	if err != nil {
		panic(fmt.Sprintf("invalid UUID from Authlier record: %q", raw))
	}
	return value
}
