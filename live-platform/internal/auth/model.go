package auth

import "time"

type User struct {
	ID int64 `json:"id"`
	// Account is the email-style identifier used by the FluxFeed-compatible
	// authentication API. Username stays in the response for older clients.
	Account      string    `json:"account"`
	Username     string    `json:"username"`
	Nickname     string    `json:"nickname"`
	Avatar       string    `json:"avatar"`
	PasswordHash string    `json:"-"`
	Status       int       `json:"-"`
	CreatedAt    time.Time `json:"created_at"`
}

// ExternalUser is the minimal profile mirrored from the platform identity
// provider. It never carries a password or an access token.
type ExternalUser struct {
	Provider string
	Subject  string
	Username string
	Nickname string
	Avatar   string
}
