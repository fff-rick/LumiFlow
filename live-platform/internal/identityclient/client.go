package identityclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/example/live-platform/internal/auth"
	"github.com/example/live-platform/internal/identityprojection"
	"github.com/example/live-platform/internal/room"
	"net/http"
	"strings"
	"time"
)

type Client struct {
	base          string
	internalToken string
	http          *http.Client
}

func New(base string) *Client {
	return &Client{base: strings.TrimRight(base, "/"), http: &http.Client{Timeout: 800 * time.Millisecond}}
}
func NewInternal(base, token string) *Client {
	client := New(base)
	client.internalToken = token
	return client
}
func (c *Client) call(ctx context.Context, method, path string, out any) (err error) {
	r, err := http.NewRequestWithContext(ctx, method, c.base+path, nil)
	if err != nil {
		return err
	}
	if c.internalToken != "" {
		r.Header.Set("X-Internal-Token", c.internalToken)
	}
	res, err := c.http.Do(r)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := res.Body.Close(); err == nil && closeErr != nil {
			err = fmt.Errorf("close identity-room response body: %w", closeErr)
		}
	}()
	if res.StatusCode/100 != 2 {
		return fmt.Errorf("identity-room status=%d", res.StatusCode)
	}
	return json.NewDecoder(res.Body).Decode(out)
}
func (c *Client) SyncFluxFeed(ctx context.Context, projection identityprojection.Projection) error {
	body, err := json.Marshal(struct {
		ID        int64  `json:"id"`
		Account   string `json:"account"`
		Nickname  string `json:"nickname"`
		AvatarURL string `json:"avatar_url"`
		Enabled   bool   `json:"enabled"`
	}{projection.ID, projection.Account, projection.Nickname, projection.AvatarURL, projection.Enabled})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/internal/v1/users/fluxfeed-sync", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Internal-Token", c.internalToken)
	res, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode/100 != 2 {
		return fmt.Errorf("identity-room status=%d", res.StatusCode)
	}
	return nil
}
func (c *Client) Get(ctx context.Context, id int64) (room.Room, error) {
	var v room.Room
	e := c.call(ctx, "GET", fmt.Sprintf("/internal/v1/rooms/%d", id), &v)
	return v, e
}
func (c *Client) GetRoomAccess(ctx context.Context, r, u int64) (room.RoomAccess, error) {
	var v room.RoomAccess
	e := c.call(ctx, "GET", fmt.Sprintf("/internal/v1/rooms/%d/access/%d", r, u), &v)
	return v, e
}
func (c *Client) IsBanned(ctx context.Context, r, u int64) (bool, error) {
	v, e := c.GetRoomAccess(ctx, r, u)
	return v.Banned, e
}
func (c *Client) IsMuted(ctx context.Context, r, u int64) (bool, error) {
	v, e := c.GetRoomAccess(ctx, r, u)
	return v.Muted, e
}
func (c *Client) Join(ctx context.Context, r, u int64) (room.Room, error) {
	var v room.Room
	e := c.call(ctx, "POST", fmt.Sprintf("/internal/v1/rooms/%d/join/%d", r, u), &v)
	return v, e
}
func (c *Client) User(ctx context.Context, id int64) (auth.User, error) {
	var v auth.User
	e := c.call(ctx, "GET", fmt.Sprintf("/internal/v1/users/%d", id), &v)
	return v, e
}
func (c *Client) Create(context.Context, int64, string) (room.Room, error) {
	return room.Room{}, fmt.Errorf("unsupported")
}
func (c *Client) List(context.Context, room.Status, int) ([]room.Room, error) {
	return nil, fmt.Errorf("unsupported")
}
func (c *Client) Start(context.Context, int64, int64) (room.Room, error) {
	return room.Room{}, fmt.Errorf("unsupported")
}
func (c *Client) Stop(context.Context, int64, int64) (room.Room, error) {
	return room.Room{}, fmt.Errorf("unsupported")
}
func (c *Client) Mute(context.Context, int64, int64, int64, time.Duration, string) error {
	return fmt.Errorf("unsupported")
}
func (c *Client) Unmute(context.Context, int64, int64, int64) error { return fmt.Errorf("unsupported") }
func (c *Client) ListMutes(context.Context, int64, int64) ([]room.Mute, error) {
	return nil, fmt.Errorf("unsupported")
}
func (c *Client) Ban(context.Context, int64, int64, int64, string) error {
	return fmt.Errorf("unsupported")
}
func (c *Client) Unban(context.Context, int64, int64, int64) error { return fmt.Errorf("unsupported") }
func (c *Client) ListBans(context.Context, int64, int64) ([]room.Ban, error) {
	return nil, fmt.Errorf("unsupported")
}
