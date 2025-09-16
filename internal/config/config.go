package config

import (
	env "github.com/caarlos0/env/v11"
)

type DatabaseType string

const (
	DatabaseTypePostgreSQL DatabaseType = "postgresql"
	DatabaseTypeMemory     DatabaseType = "memory"
)

// Config holds the application configuration
// See .env.example for more documentation
type Config struct {
	ServerAddress            string       `env:"SERVER_ADDRESS" envDefault:":8080"`
	DatabaseType             DatabaseType `env:"DATABASE_TYPE" envDefault:"postgresql"`
	DatabaseURL              string       `env:"DATABASE_URL" envDefault:"postgres://localhost:5432/mcp-registry?sslmode=disable"`
	SeedFrom                 string       `env:"SEED_FROM" envDefault:""`
	Version                  string       `env:"VERSION" envDefault:"dev"`
	GithubClientID           string       `env:"GITHUB_CLIENT_ID" envDefault:""`
	GithubClientSecret       string       `env:"GITHUB_CLIENT_SECRET" envDefault:""`
	JWTPrivateKey            string       `env:"JWT_PRIVATE_KEY" envDefault:""`
	EnableAnonymousAuth      bool         `env:"ENABLE_ANONYMOUS_AUTH" envDefault:"false"`
	EnableRegistryValidation bool         `env:"ENABLE_REGISTRY_VALIDATION" envDefault:"true"`

	// OIDC Configuration
	OIDCEnabled      bool   `env:"OIDC_ENABLED" envDefault:"false"`
	OIDCIssuer       string `env:"OIDC_ISSUER" envDefault:""`
	OIDCClientID     string `env:"OIDC_CLIENT_ID" envDefault:""`
	OIDCClientSecret string `env:"OIDC_CLIENT_SECRET" envDefault:""`
	OIDCExtraClaims  string `env:"OIDC_EXTRA_CLAIMS" envDefault:""`
	OIDCEditPerms    string `env:"OIDC_EDIT_PERMISSIONS" envDefault:""`
	OIDCPublishPerms string `env:"OIDC_PUBLISH_PERMISSIONS" envDefault:""`
	
	// Rate Limiting Configuration
	RateLimitEnabled    bool   `env:"RATE_LIMIT_ENABLED" envDefault:"true"`
	RateLimitPerDay     int    `env:"RATE_LIMIT_PER_DAY" envDefault:"10"`
	RateLimitExemptions string `env:"RATE_LIMIT_EXEMPTIONS" envDefault:""` // comma-separated
}

// NewConfig creates a new configuration with default values
func NewConfig() *Config {
	var cfg Config
	err := env.ParseWithOptions(&cfg, env.Options{
		Prefix: "MCP_REGISTRY_",
	})
	if err != nil {
		panic(err)
	}
	return &cfg
}
