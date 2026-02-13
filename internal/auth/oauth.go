// Package auth provides OAuth2 login (GitHub, Google, Facebook) and session management.
package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/facebook"
	"golang.org/x/oauth2/github"
	"golang.org/x/oauth2/google"

	"github.com/eslider/mails/internal/model"
)

// ProviderConfig holds OAuth2 settings for a single provider.
type ProviderConfig struct {
	ClientID     string
	ClientSecret string
	RedirectURL  string
}

// Providers maps provider name to its OAuth2 config.
type Providers struct {
	configs map[string]*oauth2.Config
}

// NewProviders creates OAuth2 configs for enabled providers.
// Pass nil for any provider to disable it.
func NewProviders(baseURL string, gh, gl, fb *ProviderConfig) *Providers {
	p := &Providers{configs: make(map[string]*oauth2.Config)}

	if gh != nil {
		p.configs["github"] = &oauth2.Config{
			ClientID:     gh.ClientID,
			ClientSecret: gh.ClientSecret,
			RedirectURL:  baseURL + "/auth/github/callback",
			Scopes:       []string{"user:email"},
			Endpoint:     github.Endpoint,
		}
	}

	if gl != nil {
		p.configs["google"] = &oauth2.Config{
			ClientID:     gl.ClientID,
			ClientSecret: gl.ClientSecret,
			RedirectURL:  baseURL + "/auth/google/callback",
			Scopes:       []string{"openid", "email", "profile"},
			Endpoint:     google.Endpoint,
		}
	}

	if fb != nil {
		p.configs["facebook"] = &oauth2.Config{
			ClientID:     fb.ClientID,
			ClientSecret: fb.ClientSecret,
			RedirectURL:  baseURL + "/auth/facebook/callback",
			Scopes:       []string{"email", "public_profile"},
			Endpoint:     facebook.Endpoint,
		}
	}

	return p
}

// Config returns the OAuth2 config for a provider, or nil if not configured.
func (p *Providers) Config(provider string) *oauth2.Config {
	return p.configs[provider]
}

// AuthURL returns the OAuth2 authorization URL for the given provider and state.
func (p *Providers) AuthURL(provider, state string) (string, error) {
	cfg := p.configs[provider]
	if cfg == nil {
		return "", fmt.Errorf("unknown provider: %s", provider)
	}
	return cfg.AuthCodeURL(state, oauth2.AccessTypeOffline), nil
}

// Exchange trades an authorization code for user info from the provider.
func (p *Providers) Exchange(ctx context.Context, provider, code string) (*model.User, error) {
	cfg := p.configs[provider]
	if cfg == nil {
		return nil, fmt.Errorf("unknown provider: %s", provider)
	}

	token, err := cfg.Exchange(ctx, code)
	if err != nil {
		return nil, fmt.Errorf("oauth exchange: %w", err)
	}

	client := cfg.Client(ctx, token)

	switch provider {
	case "github":
		return fetchGitHubUser(client)
	case "google":
		return fetchGoogleUser(client)
	case "facebook":
		return fetchFacebookUser(client)
	default:
		return nil, fmt.Errorf("unsupported provider: %s", provider)
	}
}

// Available returns the list of configured provider names.
func (p *Providers) Available() []string {
	var names []string
	for name := range p.configs {
		names = append(names, name)
	}
	return names
}

func fetchGitHubUser(client *http.Client) (*model.User, error) {
	resp, err := client.Get("https://api.github.com/user")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	var gh struct {
		ID        int    `json:"id"`
		Login     string `json:"login"`
		Name      string `json:"name"`
		Email     string `json:"email"`
		AvatarURL string `json:"avatar_url"`
	}
	if err := json.Unmarshal(body, &gh); err != nil {
		return nil, err
	}

	name := gh.Name
	if name == "" {
		name = gh.Login
	}

	return &model.User{
		Name:       name,
		Email:      gh.Email,
		AvatarURL:  gh.AvatarURL,
		Provider:   "github",
		ProviderID: fmt.Sprintf("%d", gh.ID),
	}, nil
}

func fetchGoogleUser(client *http.Client) (*model.User, error) {
	resp, err := client.Get("https://www.googleapis.com/oauth2/v2/userinfo")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	var gl struct {
		ID      string `json:"id"`
		Email   string `json:"email"`
		Name    string `json:"name"`
		Picture string `json:"picture"`
	}
	if err := json.Unmarshal(body, &gl); err != nil {
		return nil, err
	}

	return &model.User{
		Name:       gl.Name,
		Email:      gl.Email,
		AvatarURL:  gl.Picture,
		Provider:   "google",
		ProviderID: gl.ID,
	}, nil
}

func fetchFacebookUser(client *http.Client) (*model.User, error) {
	resp, err := client.Get("https://graph.facebook.com/me?fields=id,name,email,picture.type(large)")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	var fb struct {
		ID      string `json:"id"`
		Name    string `json:"name"`
		Email   string `json:"email"`
		Picture struct {
			Data struct {
				URL string `json:"url"`
			} `json:"data"`
		} `json:"picture"`
	}
	if err := json.Unmarshal(body, &fb); err != nil {
		return nil, err
	}

	return &model.User{
		Name:       fb.Name,
		Email:      fb.Email,
		AvatarURL:  fb.Picture.Data.URL,
		Provider:   "facebook",
		ProviderID: fb.ID,
	}, nil
}
