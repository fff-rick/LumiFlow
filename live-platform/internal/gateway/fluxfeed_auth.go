package gateway

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/example/live-platform/internal/auth"
)

var errFluxFeedUnauthorized = errors.New("fluxfeed access token is invalid")

type fluxFeedHTTPError struct {
	status  int
	message string
}

func (e *fluxFeedHTTPError) Error() string { return e.message }

type fluxFeedProfile struct {
	ID        int64  `json:"id"`
	Account   string `json:"account"`
	Nickname  string `json:"nickname"`
	AvatarURL string `json:"avatar_url"`
}

type fluxFeedSession struct {
	AccessToken      string `json:"access_token"`
	TokenType        string `json:"token_type"`
	ExpiresInSeconds int64  `json:"expires_in_seconds"`
}

type bridgeLoginResponse struct {
	User             auth.User `json:"user"`
	AccessToken      string    `json:"access_token"`
	TokenType        string    `json:"token_type"`
	ExpiresInSeconds int64     `json:"expires_in_seconds"`
}

type fluxFeedBridge struct {
	baseURL       string
	identityURL   string
	internalToken string
	tokens        *auth.TokenManager
	http          *http.Client
	mu            sync.Mutex
	cache         map[[32]byte]cachedBridgeUser
}

type cachedBridgeUser struct {
	user      auth.User
	expiresAt time.Time
}

func newFluxFeedBridge(d Deps) (*fluxFeedBridge, error) {
	if strings.TrimSpace(d.FluxFeedURL) == "" {
		return nil, errors.New("FLUXFEED_BASE_URL is required")
	}
	if strings.TrimSpace(d.BridgeJWTSecret) == "" || d.BridgeTokenTTL <= 0 || strings.TrimSpace(d.InternalToken) == "" {
		return nil, errors.New("FluxFeed auth bridge requires a live JWT secret, token TTL and INTERNAL_SERVICE_TOKEN")
	}
	return &fluxFeedBridge{
		baseURL: strings.TrimRight(strings.TrimSpace(d.FluxFeedURL), "/"), identityURL: strings.TrimRight(strings.TrimSpace(d.IdentityRoomURL), "/"),
		internalToken: d.InternalToken, tokens: auth.NewTokenManager(d.BridgeJWTSecret, d.BridgeTokenTTL), http: &http.Client{Timeout: 2 * time.Second}, cache: map[[32]byte]cachedBridgeUser{},
	}, nil
}

func (b *fluxFeedBridge) login(ctx context.Context, account, password string) (bridgeLoginResponse, error) {
	var session fluxFeedSession
	if err := b.json(ctx, http.MethodPost, "/api/sessions", map[string]string{"account": account, "password": password}, "", &session); err != nil {
		return bridgeLoginResponse{}, err
	}
	profile, err := b.profile(ctx, session.AccessToken)
	if err != nil {
		return bridgeLoginResponse{}, err
	}
	user, err := b.sync(ctx, profile)
	if err != nil {
		return bridgeLoginResponse{}, err
	}
	return bridgeLoginResponse{User: user, AccessToken: session.AccessToken, TokenType: session.TokenType, ExpiresInSeconds: session.ExpiresInSeconds}, nil
}

func (b *fluxFeedBridge) register(ctx context.Context, account, password, nickname string) (bridgeLoginResponse, error) {
	if err := b.json(ctx, http.MethodPost, "/api/users", map[string]string{"account": account, "password": password, "nickname": nickname}, "", &struct{}{}); err != nil {
		return bridgeLoginResponse{}, err
	}
	return b.login(ctx, account, password)
}

func (b *fluxFeedBridge) liveToken(ctx context.Context, authorization string) (string, error) {
	cacheKey := sha256.Sum256([]byte(authorization))
	if user, ok := b.cachedUser(cacheKey); ok {
		token, _, err := b.tokens.Issue(user.ID)
		return token, err
	}
	profile, err := b.profile(ctx, authorization)
	if err != nil {
		return "", err
	}
	user, err := b.sync(ctx, profile)
	if err != nil {
		return "", err
	}
	b.storeCachedUser(cacheKey, user)
	token, _, err := b.tokens.Issue(user.ID)
	if err != nil {
		return "", err
	}
	return token, nil
}

func (b *fluxFeedBridge) cachedUser(key [32]byte) (auth.User, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	entry, ok := b.cache[key]
	if !ok || !entry.expiresAt.After(time.Now()) {
		delete(b.cache, key)
		return auth.User{}, false
	}
	return entry.user, true
}

func (b *fluxFeedBridge) storeCachedUser(key [32]byte, user auth.User) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.cache) >= 10_000 {
		for oldKey, entry := range b.cache {
			if !entry.expiresAt.After(time.Now()) {
				delete(b.cache, oldKey)
			}
		}
	}
	// shortcut: a 30-second identity cache bounds provider load; replace with
	// signed asymmetric access tokens when FluxFeed publishes a verification key.
	b.cache[key] = cachedBridgeUser{user: user, expiresAt: time.Now().Add(30 * time.Second)}
}

func (b *fluxFeedBridge) profile(ctx context.Context, token string) (fluxFeedProfile, error) {
	if strings.TrimSpace(token) == "" {
		return fluxFeedProfile{}, errFluxFeedUnauthorized
	}
	var profile fluxFeedProfile
	if err := b.json(ctx, http.MethodGet, "/api/users/me", nil, token, &profile); err != nil {
		return fluxFeedProfile{}, err
	}
	if profile.ID <= 0 || strings.TrimSpace(profile.Nickname) == "" {
		return fluxFeedProfile{}, errors.New("invalid FluxFeed profile")
	}
	return profile, nil
}

func (b *fluxFeedBridge) sync(ctx context.Context, profile fluxFeedProfile) (auth.User, error) {
	payload := map[string]any{"id": profile.ID, "account": profile.Account, "nickname": profile.Nickname, "avatar_url": profile.AvatarURL}
	var user auth.User
	if err := b.internalJSON(ctx, http.MethodPost, "/internal/v1/users/fluxfeed-sync", payload, &user); err != nil {
		return auth.User{}, err
	}
	return user, nil
}

func (b *fluxFeedBridge) json(ctx context.Context, method, path string, body any, token string, output any) error {
	return b.call(ctx, b.baseURL+path, method, body, token, "", output)
}

func (b *fluxFeedBridge) internalJSON(ctx context.Context, method, path string, body any, output any) error {
	return b.call(ctx, b.identityURL+path, method, body, "", b.internalToken, output)
}

func (b *fluxFeedBridge) call(ctx context.Context, rawURL, method string, body any, token, internalToken string, output any) error {
	var reader io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, rawURL, reader)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if internalToken != "" {
		req.Header.Set("X-Internal-Token", internalToken)
	}
	res, err := b.http.Do(req)
	if err != nil {
		return fmt.Errorf("identity provider unavailable: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusUnauthorized || res.StatusCode == http.StatusForbidden {
		return errFluxFeedUnauthorized
	}
	if res.StatusCode/100 != 2 {
		var payload struct {
			Error string `json:"error"`
		}
		_ = json.NewDecoder(io.LimitReader(res.Body, 8<<10)).Decode(&payload)
		if payload.Error == "" {
			payload.Error = fmt.Sprintf("identity provider status=%d", res.StatusCode)
		}
		return &fluxFeedHTTPError{status: res.StatusCode, message: payload.Error}
	}
	if output == nil {
		return nil
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 64<<10)).Decode(output); err != nil {
		return fmt.Errorf("decode identity provider response: %w", err)
	}
	return nil
}
