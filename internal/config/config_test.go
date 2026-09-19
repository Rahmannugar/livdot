package config

import "testing"

func TestValidateRejectsMissingDependencies(t *testing.T) {
	tests := []struct {
		name string
		cfg  Config
	}{
		{
			name: "database host",
			cfg: Config{
				Environment: EnvironmentDevelopment,
				HTTP:        HTTP{Port: 8080},
				Database: Database{
					Port:           5432,
					User:           "livdot",
					Password:       "livdot",
					Name:           "livdot",
					MaxConnections: 20,
				},
				Redis: Redis{Address: "localhost:6379"},
			},
		},
		{
			name: "Redis address",
			cfg: Config{
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
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := test.cfg.Validate(); err == nil {
				t.Fatalf("Validate() error = nil, want error for missing %s", test.name)
			}
		})
	}
}

func TestValidateAcceptsCompleteConfiguration(t *testing.T) {
	cfg := Config{
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
		Redis: Redis{Address: "localhost:6379"},
	}

	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}
