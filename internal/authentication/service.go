package authentication

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Rahmannugar/authlier/emailaddress"
	"github.com/Rahmannugar/authlier/emailpassword"
	authpassword "github.com/Rahmannugar/authlier/password"
	"github.com/Rahmannugar/authlier/sessiontoken"
	authrepo "github.com/Rahmannugar/livdot/internal/authentication/repositories"
	authenticationdb "github.com/Rahmannugar/livdot/internal/authentication/repositories/generated"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

type Role string

const (
	RoleHost          Role = "host"
	RoleCrew          Role = "crew"
	RoleUser          Role = "user"
	RoleInternalAdmin Role = "internal_admin"
)

var (
	ErrRegistrationDisabled = errors.New("registration is disabled for this role")
	ErrInvalidRole          = errors.New("invalid authentication role")
	ErrInvalidProfile       = errors.New("invalid profile details")
	ErrUnauthenticated      = errors.New("session token is not valid for an active session")
)

// the authenticated account behind a session token.
type Identity struct {
	AccountID string
	Role      Role
}

type Credentials struct {
	Email     string
	Password  string
	FullName  string
	CrewName  string
	SourceKey string
}

type Result struct {
	AccountID string    `json:"accountId"`
	Email     string    `json:"email"`
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expiresAt"`
}

type InternalAdminInput struct {
	Email    string
	Password string
	FullName string
	Role     authenticationdb.InternalAdminRole
}

type Service struct {
	accounts  map[Role]*roleAuthenticator
	internal  *roleAuthenticator
	sessions  *sessiontoken.Manager
	directory *authrepo.AccountDirectory
}

type roleAuthenticator struct {
	role    Role
	store   *authrepo.AccountStore
	manager *emailpassword.Manager
}

func NewService(
	pool *pgxpool.Pool,
	redisClient *redis.Client,
	sessionLifetime time.Duration,
	sessionCacheTTL time.Duration,
) (*Service, error) {
	if pool == nil {
		return nil, fmt.Errorf("authentication database pool is required")
	}
	if sessionLifetime <= 0 {
		return nil, fmt.Errorf("authentication session lifetime must be positive")
	}
	if redisClient != nil && sessionCacheTTL <= 0 {
		return nil, fmt.Errorf("authentication session cache TTL must be positive")
	}

	sessionStore := authrepo.NewSessionStore(pool)
	var sessionCache sessiontoken.Cache
	if redisClient != nil {
		sessionCache = authrepo.NewSessionCache(redisClient)
	}
	sessions, err := sessiontoken.NewManager(sessionStore, sessionCache, sessiontoken.Config{
		Lifetime: sessionLifetime,
		CacheTTL: sessionCacheTTL,
	})
	if err != nil {
		return nil, fmt.Errorf("configure session manager: %w", err)
	}

	service := &Service{
		accounts:  make(map[Role]*roleAuthenticator, 3),
		sessions:  sessions,
		directory: authrepo.NewAccountDirectory(pool),
	}
	for role, accountType := range map[Role]authenticationdb.AuthenticationAccountType{
		RoleHost: authenticationdb.AuthenticationAccountTypeHost,
		RoleCrew: authenticationdb.AuthenticationAccountTypeCrew,
		RoleUser: authenticationdb.AuthenticationAccountTypeUser,
	} {
		authenticator, err := newRoleAuthenticator(role, authrepo.NewAccountStore(pool, accountType))
		if err != nil {
			return nil, err
		}
		service.accounts[role] = authenticator
	}
	internal, err := newRoleAuthenticator(
		RoleInternalAdmin,
		authrepo.NewAccountStore(pool, authenticationdb.AuthenticationAccountTypeInternalAdmin),
	)
	if err != nil {
		return nil, err
	}
	service.internal = internal
	return service, nil
}

func newRoleAuthenticator(role Role, store *authrepo.AccountStore) (*roleAuthenticator, error) {
	manager, err := emailpassword.NewManager(store, emailpassword.Config{
		ValidatePassword: validatePassword,
	})
	if err != nil {
		return nil, fmt.Errorf("configure %s authentication: %w", role, err)
	}
	return &roleAuthenticator{role: role, store: store, manager: manager}, nil
}

func (service *Service) Register(ctx context.Context, role Role, credentials Credentials) (Result, error) {
	authenticator, ok := service.accounts[role]
	if !ok {
		if role == RoleInternalAdmin {
			return Result{}, ErrRegistrationDisabled
		}
		return Result{}, ErrInvalidRole
	}
	if err := validateRegistration(role, credentials); err != nil {
		return Result{}, err
	}
	ctx = authrepo.WithRegistrationDetails(ctx, authrepo.RegistrationDetails{
		FullName: credentials.FullName,
		CrewName: credentials.CrewName,
	})
	registered, err := authenticator.manager.Register(ctx, emailpassword.RegisterInput{
		Email:     credentials.Email,
		Password:  credentials.Password,
		SourceKey: credentials.SourceKey,
	})
	if err != nil {
		return Result{}, err
	}
	return service.issueSession(ctx, registered.ID, registered.Email)
}

func (service *Service) SignIn(ctx context.Context, role Role, credentials Credentials) (Result, error) {
	authenticator, ok := service.accounts[role]
	if !ok {
		if role != RoleInternalAdmin {
			return Result{}, ErrInvalidRole
		}
		authenticator = service.internal
	}
	result, err := authenticator.manager.Login(ctx, emailpassword.LoginInput{
		Email:     credentials.Email,
		Password:  credentials.Password,
		SourceKey: credentials.SourceKey,
	})
	if err != nil {
		return Result{}, err
	}
	return service.issueSession(ctx, result.User.ID, result.User.Email)
}

func (service *Service) Resolve(ctx context.Context, rawToken string) (sessiontoken.Record, error) {
	return service.sessions.Resolve(ctx, rawToken)
}

// resolves a session token to the account + role. bad tokens come back as
// ErrUnauthenticated so callers can tell a rejected token from an outage.
func (service *Service) Authenticate(ctx context.Context, rawToken string) (Identity, error) {
	if strings.TrimSpace(rawToken) == "" {
		return Identity{}, ErrUnauthenticated
	}
	record, err := service.sessions.Resolve(ctx, rawToken)
	switch {
	case errors.Is(err, sessiontoken.ErrNotFound),
		errors.Is(err, sessiontoken.ErrInactive),
		errors.Is(err, sessiontoken.ErrInvalidToken),
		errors.Is(err, sessiontoken.ErrInvalidRecord):
		return Identity{}, ErrUnauthenticated
	case err != nil:
		return Identity{}, err
	}

	accountID, err := uuid.Parse(record.SubjectID)
	if err != nil {
		return Identity{}, ErrUnauthenticated
	}
	accountType, err := service.directory.AccountType(ctx, accountID)
	if errors.Is(err, authrepo.ErrAccountNotFound) {
		return Identity{}, ErrUnauthenticated
	}
	if err != nil {
		return Identity{}, err
	}
	role, ok := roleForAccountType(accountType)
	if !ok {
		return Identity{}, ErrUnauthenticated
	}
	return Identity{AccountID: record.SubjectID, Role: role}, nil
}

func (service *Service) Revoke(ctx context.Context, rawToken string) error {
	return service.sessions.Revoke(ctx, rawToken)
}

func (service *Service) CreateInternalAdmin(ctx context.Context, input InternalAdminInput) error {
	normalizedEmail, err := emailaddress.Normalize(input.Email)
	if err != nil {
		return emailpassword.ErrInvalidInput
	}
	if err := validatePassword(input.Password); err != nil {
		return err
	}
	if strings.TrimSpace(input.FullName) == "" {
		return ErrInvalidProfile
	}
	if input.Role != authenticationdb.InternalAdminRoleAdmin && input.Role != authenticationdb.InternalAdminRoleSubadmin {
		return ErrInvalidProfile
	}
	hash, err := authpassword.Hash(input.Password)
	if err != nil {
		return fmt.Errorf("hash internal admin password: %w", err)
	}
	_, err = service.internal.store.CreateInternalAdmin(ctx, normalizedEmail, hash, strings.TrimSpace(input.FullName), input.Role)
	return err
}

func (service *Service) issueSession(ctx context.Context, accountID, email string) (Result, error) {
	issued, err := service.sessions.Create(ctx, accountID)
	if err != nil {
		return Result{}, fmt.Errorf("create authentication session: %w", err)
	}
	return Result{
		AccountID: accountID,
		Email:     email,
		Token:     issued.Token,
		ExpiresAt: issued.Record.ExpiresAt,
	}, nil
}

func validateRegistration(role Role, credentials Credentials) error {
	if role != RoleHost && role != RoleCrew && role != RoleUser {
		return ErrInvalidRole
	}
	if role == RoleCrew {
		if strings.TrimSpace(credentials.CrewName) == "" {
			return ErrInvalidProfile
		}
		return nil
	}
	if strings.TrimSpace(credentials.FullName) == "" {
		return ErrInvalidProfile
	}
	return nil
}

func validatePassword(plainPassword string) error {
	if utf8.RuneCountInString(plainPassword) < 8 {
		return fmt.Errorf("password must contain at least 8 characters")
	}
	return nil
}

func roleForAccountType(accountType authenticationdb.AuthenticationAccountType) (Role, bool) {
	switch accountType {
	case authenticationdb.AuthenticationAccountTypeHost:
		return RoleHost, true
	case authenticationdb.AuthenticationAccountTypeCrew:
		return RoleCrew, true
	case authenticationdb.AuthenticationAccountTypeUser:
		return RoleUser, true
	case authenticationdb.AuthenticationAccountTypeInternalAdmin:
		return RoleInternalAdmin, true
	default:
		return "", false
	}
}
