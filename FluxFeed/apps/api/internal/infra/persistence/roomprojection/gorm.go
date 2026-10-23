package roomprojection

import (
	applicationeventbus "FluxFeed/internal/application/eventbus"
	"context"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Model struct {
	RoomID         int64      `gorm:"column:room_id;primaryKey"`
	LumiAnchorID   int64      `gorm:"column:lumi_anchor_id;not null;index"`
	AnchorNickname string     `gorm:"column:anchor_nickname;size:64;not null"`
	Title          string     `gorm:"column:title;size:255;not null"`
	Status         string     `gorm:"column:status;size:16;not null;index"`
	StartedAt      *time.Time `gorm:"column:started_at"`
	EndedAt        *time.Time `gorm:"column:ended_at"`
	UpdatedAt      time.Time  `gorm:"column:updated_at;not null"`
}

func (Model) TableName() string { return "live_room_projection" }

type Store interface {
	Apply(context.Context, Model) error
}

type GormStore struct{ db *gorm.DB }

func New(db *gorm.DB) *GormStore { return &GormStore{db: db} }

func (s *GormStore) Apply(ctx context.Context, value Model) error {
	return s.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "room_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"lumi_anchor_id", "anchor_nickname", "title", "status", "started_at", "ended_at", "updated_at"}),
	}).Create(&value).Error
}

type Handler struct{ store Store }

func NewHandler(store Store) *Handler { return &Handler{store: store} }

func (h *Handler) Handle(ctx context.Context, event *applicationeventbus.Event) error {
	if event == nil || event.RoomID <= 0 || event.UserID <= 0 || (event.Type != applicationeventbus.TypeIntegrationRoomStarted && event.Type != applicationeventbus.TypeIntegrationRoomStopped && event.Type != applicationeventbus.TypeIntegrationRoomOwnerChanged) {
		return errors.New("invalid integration.room.v1 event")
	}
	var payload struct {
		Title          string     `json:"title"`
		AnchorNickname string     `json:"anchor_nickname"`
		Status         string     `json:"status"`
		StartedAt      *time.Time `json:"started_at"`
		EndedAt        *time.Time `json:"ended_at"`
	}
	if err := event.Decode(&payload); err != nil {
		return err
	}
	payload.Title, payload.Status = strings.TrimSpace(payload.Title), strings.ToUpper(strings.TrimSpace(payload.Status))
	if payload.Title == "" || len([]rune(payload.Title)) > 100 || (payload.Status != "PREPARING" && payload.Status != "LIVING" && payload.Status != "CLOSED") {
		return errors.New("invalid integration.room.v1 payload")
	}
	if event.Type == applicationeventbus.TypeIntegrationRoomStarted && (payload.Status != "LIVING" || payload.StartedAt == nil) {
		return errors.New("invalid room.started payload")
	}
	if event.Type == applicationeventbus.TypeIntegrationRoomStopped && (payload.Status != "CLOSED" || payload.EndedAt == nil) {
		return errors.New("invalid room.stopped payload")
	}
	return h.store.Apply(ctx, Model{
		RoomID: event.RoomID, LumiAnchorID: event.UserID, AnchorNickname: strings.TrimSpace(payload.AnchorNickname), Title: payload.Title,
		Status: payload.Status, StartedAt: payload.StartedAt, EndedAt: payload.EndedAt, UpdatedAt: time.Now().UTC(),
	})
}
