package identityprojection

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/example/live-platform/internal/eventdedup"
	"github.com/example/live-platform/internal/mq"
)

const Producer = "fluxfeed"

type Deduper interface {
	Begin(context.Context, string, string, time.Duration) (bool, error)
	Done(context.Context, string, string) error
	Fail(context.Context, string, string, error) error
}

type Projector interface {
	SyncFluxFeed(context.Context, Projection) error
}

type Projection struct {
	ID        int64
	Account   string
	Nickname  string
	AvatarURL string
	Enabled   bool
}

type Handler struct {
	group   string
	dedup   Deduper
	project Projector
	lease   time.Duration
}

type envelope struct {
	EventID    string    `json:"event_id"`
	EventType  string    `json:"event_type"`
	Version    int       `json:"version"`
	OccurredAt time.Time `json:"occurred_at"`
	Producer   string    `json:"producer"`
	Subject    struct {
		FluxFeedUserID int64 `json:"fluxfeed_user_id"`
	} `json:"subject"`
	Payload struct {
		Account   string `json:"account"`
		Nickname  string `json:"nickname"`
		AvatarURL string `json:"avatar_url"`
	} `json:"payload"`
}

func NewHandler(group string, dedup Deduper, project Projector, lease time.Duration) *Handler {
	return &Handler{group: group, dedup: dedup, project: project, lease: lease}
}

func (h *Handler) Handle(ctx context.Context, record mq.Record) error {
	var event envelope
	if err := json.Unmarshal(record.Value, &event); err != nil {
		return mq.Permanent(fmt.Errorf("decode identity event: %w", err))
	}
	if err := event.validate(); err != nil {
		return mq.Permanent(err)
	}
	done, err := h.dedup.Begin(ctx, h.group, event.EventID, h.lease)
	if err != nil {
		return err
	}
	if done {
		return nil
	}
	err = h.project.SyncFluxFeed(ctx, Projection{ID: event.Subject.FluxFeedUserID, Account: event.Payload.Account, Nickname: event.Payload.Nickname, AvatarURL: event.Payload.AvatarURL, Enabled: event.EventType != "user.disabled"})
	if err != nil {
		_ = h.dedup.Fail(ctx, h.group, event.EventID, err)
		return err
	}
	if err := h.dedup.Done(ctx, h.group, event.EventID); err != nil {
		return err
	}
	return nil
}

func (event envelope) validate() error {
	if strings.TrimSpace(event.EventID) == "" || event.Version != 1 || event.OccurredAt.IsZero() || event.Producer != Producer || event.Subject.FluxFeedUserID <= 0 || strings.TrimSpace(event.Payload.Nickname) == "" || len([]rune(event.Payload.Nickname)) > 64 || len(event.Payload.AvatarURL) > 512 {
		return errors.New("invalid integration.identity.v1 envelope")
	}
	if event.EventType != "user.profile_changed" && event.EventType != "user.disabled" {
		return fmt.Errorf("unsupported identity event type %q", event.EventType)
	}
	return nil
}

var _ Deduper = (*eventdedup.Store)(nil)
