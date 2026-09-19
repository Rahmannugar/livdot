package config

import (
	"errors"
	"fmt"
	"io/fs"
	"strconv"
	"strings"

	"github.com/knadh/koanf/parsers/dotenv"
	koanfenv "github.com/knadh/koanf/providers/env/v2"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"
)

const (
	environmentPrefix             = "LIVDOT_"
	defaultHTTPPort               = 8080
	defaultDatabasePort           = 5432
	defaultDatabaseMaxConnections = 20
)

type Environment string

const (
	EnvironmentDevelopment Environment = "development"
	EnvironmentTest        Environment = "test"
	EnvironmentProduction  Environment = "production"
)

type Config struct {
	Environment Environment
	HTTP        HTTP
	Database    Database
	Redis       Redis
}

type HTTP struct {
	Port int
}

type Database struct {
	Host           string
	Port           uint16
	User           string
	Password       string
	Name           string
	MaxConnections int32
}

type Redis struct {
	Address  string
	Password string
}

func Load() (Config, error) {
	k := koanf.New(".")
	transform := func(key, value string) (string, any) {
		key = strings.ToLower(strings.TrimPrefix(key, environmentPrefix))
		group, setting, grouped := strings.Cut(key, "_")
		if grouped {
			key = group + "." + setting
		}
		return key, value
	}

	if err := k.Load(
		file.Provider(".env"),
		dotenv.ParserEnvWithValue(environmentPrefix, ".", transform),
	); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return Config{}, fmt.Errorf("load .env: %w", err)
	}

	provider := koanfenv.Provider(".", koanfenv.Opt{
		Prefix:        environmentPrefix,
		TransformFunc: transform,
	})
	if err := k.Load(provider, nil); err != nil {
		return Config{}, fmt.Errorf("load environment: %w", err)
	}

	cfg := Config{
		Environment: EnvironmentDevelopment,
		HTTP:        HTTP{Port: defaultHTTPPort},
		Database: Database{
			Port:           defaultDatabasePort,
			MaxConnections: defaultDatabaseMaxConnections,
		},
	}
	if k.Exists("environment") {
		cfg.Environment = Environment(k.String("environment"))
	}
	if k.Exists("http.port") {
		port, err := strconv.Atoi(k.String("http.port"))
		if err != nil {
			return Config{}, fmt.Errorf("LIVDOT_HTTP_PORT must be an integer: %w", err)
		}
		cfg.HTTP.Port = port
	}
	cfg.Database.Host = k.String("database.host")
	if k.Exists("database.port") {
		port, err := strconv.ParseUint(k.String("database.port"), 10, 16)
		if err != nil {
			return Config{}, fmt.Errorf("LIVDOT_DATABASE_PORT must be an integer between 1 and 65535: %w", err)
		}
		cfg.Database.Port = uint16(port)
	}
	cfg.Database.User = k.String("database.user")
	cfg.Database.Password = k.String("database.password")
	cfg.Database.Name = k.String("database.name")
	if k.Exists("database.max_connections") {
		maxConnections, err := strconv.ParseInt(k.String("database.max_connections"), 10, 32)
		if err != nil {
			return Config{}, fmt.Errorf("LIVDOT_DATABASE_MAX_CONNECTIONS must be an integer: %w", err)
		}
		cfg.Database.MaxConnections = int32(maxConnections)
	}
	cfg.Redis.Address = k.String("redis.address")
	cfg.Redis.Password = k.String("redis.password")

	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (cfg Config) Validate() error {
	switch cfg.Environment {
	case EnvironmentDevelopment, EnvironmentTest, EnvironmentProduction:
	default:
		return fmt.Errorf("LIVDOT_ENVIRONMENT must be development, test, or production")
	}
	if cfg.HTTP.Port < 1 || cfg.HTTP.Port > 65535 {
		return fmt.Errorf("LIVDOT_HTTP_PORT must be between 1 and 65535")
	}
	if strings.TrimSpace(cfg.Database.Host) == "" {
		return fmt.Errorf("LIVDOT_DATABASE_HOST is required")
	}
	if cfg.Database.Port == 0 {
		return fmt.Errorf("LIVDOT_DATABASE_PORT must be between 1 and 65535")
	}
	if strings.TrimSpace(cfg.Database.User) == "" {
		return fmt.Errorf("LIVDOT_DATABASE_USER is required")
	}
	if cfg.Database.Password == "" {
		return fmt.Errorf("LIVDOT_DATABASE_PASSWORD is required")
	}
	if strings.TrimSpace(cfg.Database.Name) == "" {
		return fmt.Errorf("LIVDOT_DATABASE_NAME is required")
	}
	if cfg.Database.MaxConnections < 1 {
		return fmt.Errorf("LIVDOT_DATABASE_MAX_CONNECTIONS must be positive")
	}
	if strings.TrimSpace(cfg.Redis.Address) == "" {
		return fmt.Errorf("LIVDOT_REDIS_ADDRESS is required")
	}
	return nil
}

func (cfg HTTP) Address() string {
	return ":" + strconv.Itoa(cfg.Port)
}
