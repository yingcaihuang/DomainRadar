package auth

import (
	"context"
	"fmt"
	"os"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

// OIDCConfig holds the configuration for the OIDC provider.
type OIDCConfig struct {
	IssuerURL    string
	ClientID     string
	ClientSecret string
	RedirectURL  string
}

// OIDCProvider wraps the OIDC provider and OAuth2 config for Authentik integration.
type OIDCProvider struct {
	Provider     *oidc.Provider
	OAuth2Config oauth2.Config
	Verifier     *oidc.IDTokenVerifier
}

// NewOIDCProvider creates a new OIDCProvider from the given configuration.
func NewOIDCProvider(ctx context.Context, config OIDCConfig) (*OIDCProvider, error) {
	if config.IssuerURL == "" {
		return nil, fmt.Errorf("OIDC issuer URL is required")
	}
	if config.ClientID == "" {
		return nil, fmt.Errorf("OIDC client ID is required")
	}
	if config.RedirectURL == "" {
		return nil, fmt.Errorf("OIDC redirect URL is required")
	}

	provider, err := oidc.NewProvider(ctx, config.IssuerURL)
	if err != nil {
		return nil, fmt.Errorf("failed to create OIDC provider: %w", err)
	}

	oauth2Config := oauth2.Config{
		ClientID:     config.ClientID,
		ClientSecret: config.ClientSecret,
		RedirectURL:  config.RedirectURL,
		Endpoint:     provider.Endpoint(),
		Scopes:       []string{oidc.ScopeOpenID, "profile", "email", "groups"},
	}

	verifier := provider.Verifier(&oidc.Config{
		ClientID: config.ClientID,
	})

	return &OIDCProvider{
		Provider:     provider,
		OAuth2Config: oauth2Config,
		Verifier:     verifier,
	}, nil
}

// NewOIDCProviderFromEnv creates an OIDCProvider using environment variables:
// OIDC_ISSUER_URL, OIDC_CLIENT_ID, OIDC_CLIENT_SECRET, OIDC_REDIRECT_URL.
func NewOIDCProviderFromEnv(ctx context.Context) (*OIDCProvider, error) {
	config := OIDCConfig{
		IssuerURL:    os.Getenv("OIDC_ISSUER_URL"),
		ClientID:     os.Getenv("OIDC_CLIENT_ID"),
		ClientSecret: os.Getenv("OIDC_CLIENT_SECRET"),
		RedirectURL:  os.Getenv("OIDC_REDIRECT_URL"),
	}
	return NewOIDCProvider(ctx, config)
}

// Claims represents the ID token claims from Authentik.
type Claims struct {
	Sub           string   `json:"sub"`
	Email         string   `json:"email"`
	Name          string   `json:"name"`
	PreferredUser string   `json:"preferred_username"`
	Groups        []string `json:"groups"`
}
