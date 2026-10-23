package auth

import (
	"context"
	"errors"
	"testing"
	"time"
)

type memoryUsers struct{ users map[string]User }

func (r *memoryUsers) Create(_ context.Context, account, nickname, passwordHash string) (User, error) {
	if _, ok := r.users[account]; ok {
		return User{}, errors.New("duplicate key")
	}
	u := User{ID: int64(len(r.users) + 1), Account: account, Username: account, Nickname: nickname, PasswordHash: passwordHash, Status: 1}
	r.users[account] = u
	return u, nil
}

func (r *memoryUsers) ByUsername(_ context.Context, account string) (User, error) {
	u, ok := r.users[account]
	if !ok {
		return User{}, ErrUserNotFound
	}
	return u, nil
}

func (r *memoryUsers) ByID(_ context.Context, id int64) (User, error) {
	for _, u := range r.users {
		if u.ID == id {
			return u, nil
		}
	}
	return User{}, ErrUserNotFound
}

func TestRegisterUsesFluxFeedEmailAccount(t *testing.T) {
	repo := &memoryUsers{users: map[string]User{}}
	service := NewService(repo, NewTokenManager("test-secret", time.Hour))
	result, err := service.Register(context.Background(), " User@Example.com ", "User", "password123")
	if err != nil {
		t.Fatal(err)
	}
	if result.User.Account != "user@example.com" || result.User.Username != "user@example.com" {
		t.Fatalf("unexpected account: %+v", result.User)
	}
	if _, err := service.Login(context.Background(), "USER@example.com", "password123"); err != nil {
		t.Fatalf("login with normalized email: %v", err)
	}
}

func TestRegisterRejectsNonEmailAccount(t *testing.T) {
	service := NewService(&memoryUsers{users: map[string]User{}}, NewTokenManager("test-secret", time.Hour))
	if _, err := service.Register(context.Background(), "viewer_1", "Viewer", "password123"); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("err=%v, want invalid input", err)
	}
}
