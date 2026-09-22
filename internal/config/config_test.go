package config

import (
	"testing"
	"time"
)

func validConfig() Config {
	return Config{
		Environment: EnvironmentDevelopment,
		HTTP:        HTTP{Port: 8080},
		Database: Database{
			Host:           "localhost",
			Port:           5432,
			User:           "livdot",
			Password:       "livdot",
			Name:           "livdot",
			MaxConnections: 20,
		},
		Redis:   Redis{Address: "localhost:6379"},
		Session: Session{Lifetime: time.Hour, CacheTTL: time.Minute},
		Payment: Payment{Secret: "test-secret", BaseURL: "https://mock.paystack.local"},
	}
}

func TestValidateRejectsMissingDependencies(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Config)
	}{
		{
			name:   "database host",
			mutate: func(cfg *Config) { cfg.Database.Host = "" },
		},
		{
			name:   "Redis address",
			mutate: func(cfg *Config) { cfg.Redis.Address = "" },
		},
		{
			name:   "session lifetime",
			mutate: func(cfg *Config) { cfg.Session.Lifetime = 0 },
		},
		{
			name:   "session cache TTL",
			mutate: func(cfg *Config) { cfg.Session.CacheTTL = 0 },
		},
		{
			name:   "payment secret",
			mutate: func(cfg *Config) { cfg.Payment.Secret = "" },
		},
		{
			name:   "payment base URL",
			mutate: func(cfg *Config) { cfg.Payment.BaseURL = "" },
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cfg := validConfig()
			test.mutate(&cfg)
			if err := cfg.Validate(); err == nil {
				t.Fatalf("Validate() error = nil, want error for missing %s", test.name)
			}
		})
	}
}

func TestValidateAcceptsCompleteConfiguration(t *testing.T) {
	if err := validConfig().Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}
