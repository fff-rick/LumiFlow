package media

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/example/live-platform/internal/room"
)

type fakeStore struct {
	session    Session
	recordings []Recording
}

func (f *fakeStore) Save(_ context.Context, value Session) (Session, error) {
	f.session = value
	return value, nil
}
func (f *fakeStore) ByRoom(context.Context, int64) (Session, error) {
	if f.session.RoomID == 0 {
		return Session{}, ErrNotConfigured
	}
	return f.session, nil
}
func (f *fakeStore) ByStream(_ context.Context, app, stream string) (Session, error) {
	if f.session.App != app || f.session.StreamName != stream {
		return Session{}, ErrNotConfigured
	}
	return f.session, nil
}
func (f *fakeStore) SetStatus(_ context.Context, _ int64, status Status) error {
	f.session.Status = status
	return nil
}
func (f *fakeStore) SaveRecording(_ context.Context, value Recording) (Recording, error) {
	value.ID = int64(len(f.recordings) + 1)
	f.recordings = append(f.recordings, value)
	return value, nil
}
func (f *fakeStore) ListRecordings(_ context.Context, roomID int64) ([]Recording, error) {
	var items []Recording
	for _, value := range f.recordings {
		if value.RoomID == roomID {
			items = append(items, value)
		}
	}
	return items, nil
}

type fakeRooms struct{ value room.Room }

func (f fakeRooms) Get(context.Context, int64) (room.Room, error) { return f.value, nil }

func TestIssueCredentialsAndAuthorizePublish(t *testing.T) {
	store := &fakeStore{}
	service := NewService(store, fakeRooms{room.Room{ID: 12, AnchorID: 7}}, Config{
		App: "live", RTMPServerURL: "rtmp://localhost:1935/live", WHEPBaseURL: "http://localhost:1985/rtc/v1/whep/",
	})
	credentials, err := service.IssueCredentials(context.Background(), 12, 7)
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.SplitN(credentials.StreamKey, "?", 2)
	if len(parts) != 2 || parts[0] != "room-12" {
		t.Fatalf("stream key=%q", credentials.StreamKey)
	}
	if err := service.HandleSRSCallback(context.Background(), SRSCallback{
		Action: "on_publish", App: "live", Stream: "room-12", Param: "?" + parts[1],
	}); err != nil {
		t.Fatal(err)
	}
	if store.session.Status != StatusPublishing {
		t.Fatalf("status=%s", store.session.Status)
	}
	if err := service.RequirePublishing(context.Background(), 12); err != nil {
		t.Fatal(err)
	}
}

func TestPublishRejectsWrongToken(t *testing.T) {
	store := &fakeStore{}
	service := NewService(store, fakeRooms{room.Room{ID: 12, AnchorID: 7}}, Config{App: "live"})
	if _, err := service.IssueCredentials(context.Background(), 12, 7); err != nil {
		t.Fatal(err)
	}
	err := service.HandleSRSCallback(context.Background(), SRSCallback{
		Action: "on_publish", App: "live", Stream: "room-12", Param: "?token=wrong",
	})
	if !errors.Is(err, ErrInvalidPublishToken) {
		t.Fatalf("err=%v", err)
	}
}

func TestLegacyRoomCanStartWithoutMediaSession(t *testing.T) {
	service := NewService(&fakeStore{}, fakeRooms{}, Config{})
	if err := service.RequirePublishing(context.Background(), 99); err != nil {
		t.Fatal(err)
	}
}

func TestPlaybackRequiresLivingRoom(t *testing.T) {
	store := &fakeStore{session: Session{RoomID: 12, App: "live", StreamName: "room-12", Status: StatusPublishing}}
	config := Config{App: "live", WHEPBaseURL: "http://localhost:1985/rtc/v1/whep/"}
	preparing := NewService(store, fakeRooms{room.Room{ID: 12, Status: room.StatusPreparing}}, config)
	playback, err := preparing.Playback(context.Background(), 12)
	if err != nil {
		t.Fatal(err)
	}
	if playback.PlaybackURL != "" {
		t.Fatalf("preparing room exposed playback URL %q", playback.PlaybackURL)
	}
	if err := preparing.HandleSRSCallback(context.Background(), SRSCallback{Action: "on_play", App: "live", Stream: "room-12"}); !errors.Is(err, ErrNotPublishing) {
		t.Fatalf("on_play err=%v", err)
	}

	living := NewService(store, fakeRooms{room.Room{ID: 12, Status: room.StatusLiving}}, config)
	playback, err = living.Playback(context.Background(), 12)
	if err != nil {
		t.Fatal(err)
	}
	if playback.PlaybackURL == "" {
		t.Fatal("living room did not expose playback URL")
	}
	if err := living.HandleSRSCallback(context.Background(), SRSCallback{Action: "on_play", App: "live", Stream: "room-12"}); err != nil {
		t.Fatal(err)
	}
}

func TestDVRCallbackPublishesRecordingURL(t *testing.T) {
	store := &fakeStore{session: Session{RoomID: 12, Provider: "srs", App: "live", StreamName: "room-12"}}
	service := NewService(store, fakeRooms{room.Room{ID: 12}}, Config{
		App: "live", RecordingBaseURL: "http://localhost:8085/recordings/",
	})
	if err := service.HandleSRSCallback(context.Background(), SRSCallback{
		Action: "on_dvr", App: "live", Stream: "room-12",
		File: "./objs/nginx/html/recordings/live/room-12.1793520000000.mp4",
	}); err != nil {
		t.Fatal(err)
	}
	items, err := service.Recordings(context.Background(), 12)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].PlaybackURL != "http://localhost:8085/recordings/live/room-12.1793520000000.mp4" {
		t.Fatalf("recordings=%+v", items)
	}
}

func TestDVRCallbackRejectsMismatchedFile(t *testing.T) {
	store := &fakeStore{session: Session{RoomID: 12, Provider: "srs", App: "live", StreamName: "room-12"}}
	service := NewService(store, fakeRooms{}, Config{App: "live"})
	err := service.HandleSRSCallback(context.Background(), SRSCallback{
		Action: "on_dvr", App: "live", Stream: "room-12", File: "/recordings/live/room-99.1.mp4",
	})
	if !errors.Is(err, ErrInvalidCallback) {
		t.Fatalf("err=%v", err)
	}
}
