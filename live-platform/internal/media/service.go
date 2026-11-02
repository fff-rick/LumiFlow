package media

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"path"
	"strconv"
	"strings"

	"github.com/example/live-platform/internal/room"
)

var (
	ErrNotConfigured       = errors.New("media session is not configured")
	ErrNotPublishing       = errors.New("media stream is not publishing")
	ErrAlreadyPublishing   = errors.New("media stream is already publishing")
	ErrInvalidPublishToken = errors.New("invalid publish token")
	ErrInvalidCallback     = errors.New("invalid media callback")
)

type Store interface {
	Save(context.Context, Session) (Session, error)
	ByRoom(context.Context, int64) (Session, error)
	ByStream(context.Context, string, string) (Session, error)
	SetStatus(context.Context, int64, Status) error
	SaveRecording(context.Context, Recording) (Recording, error)
	ListRecordings(context.Context, int64) ([]Recording, error)
}

type RoomReader interface {
	Get(context.Context, int64) (room.Room, error)
}

type Config struct {
	App              string
	RTMPServerURL    string
	WHEPBaseURL      string
	HLSBaseURL       string
	RecordingBaseURL string
}

type Service struct {
	store  Store
	rooms  RoomReader
	config Config
}

func NewService(store Store, rooms RoomReader, cfg Config) *Service {
	cfg.App = strings.TrimSpace(cfg.App)
	cfg.RTMPServerURL = strings.TrimRight(strings.TrimSpace(cfg.RTMPServerURL), "/")
	cfg.WHEPBaseURL = strings.TrimSpace(cfg.WHEPBaseURL)
	cfg.HLSBaseURL = strings.TrimRight(strings.TrimSpace(cfg.HLSBaseURL), "/")
	cfg.RecordingBaseURL = strings.TrimRight(strings.TrimSpace(cfg.RecordingBaseURL), "/")
	return &Service{store: store, rooms: rooms, config: cfg}
}

func (s *Service) IssueCredentials(ctx context.Context, roomID, actorID int64) (Credentials, error) {
	v, err := s.rooms.Get(ctx, roomID)
	if err != nil {
		return Credentials{}, err
	}
	if v.AnchorID != actorID {
		return Credentials{}, room.ErrForbidden
	}
	if v.Status == room.StatusClosed {
		return Credentials{}, room.ErrInvalidState
	}
	if current, err := s.store.ByRoom(ctx, roomID); err == nil && current.Status == StatusPublishing {
		return Credentials{}, ErrAlreadyPublishing
	} else if err != nil && !errors.Is(err, ErrNotConfigured) {
		return Credentials{}, err
	}
	token, err := randomToken()
	if err != nil {
		return Credentials{}, err
	}
	streamName := "room-" + strconv.FormatInt(roomID, 10)
	hash := sha256.Sum256([]byte(token))
	session, err := s.store.Save(ctx, Session{
		RoomID: roomID, Provider: "srs", App: s.config.App, StreamName: streamName,
		PublishHash: hash[:], Status: StatusCreated,
	})
	if err != nil {
		return Credentials{}, err
	}
	return Credentials{
		RoomID: roomID, Provider: session.Provider, Status: session.Status,
		ServerURL:   s.config.RTMPServerURL,
		StreamKey:   streamName + "?token=" + url.QueryEscape(token),
		PlaybackURL: s.playbackURL(streamName),
	}, nil
}

func (s *Service) Playback(ctx context.Context, roomID int64) (Playback, error) {
	v, err := s.rooms.Get(ctx, roomID)
	if err != nil {
		return Playback{}, err
	}
	session, err := s.store.ByRoom(ctx, roomID)
	if errors.Is(err, ErrNotConfigured) {
		return Playback{RoomID: roomID}, nil
	}
	if err != nil {
		return Playback{}, err
	}
	playback := Playback{
		RoomID: roomID, Provider: session.Provider, Status: session.Status,
	}
	if v.Status == room.StatusLiving && session.Status == StatusPublishing {
		playback.PlaybackURL = s.playbackURL(session.StreamName)
		playback.HLSURL = s.adaptiveURL(session.StreamName)
	}
	return playback, nil
}

func (s *Service) adaptiveURL(streamName string) string {
	if s.config.HLSBaseURL == "" {
		return ""
	}
	return s.config.HLSBaseURL + "/" + url.PathEscape(s.config.App) + "/" + url.PathEscape(streamName) + "/master.m3u8"
}

func (s *Service) Recordings(ctx context.Context, roomID int64) ([]Recording, error) {
	if _, err := s.rooms.Get(ctx, roomID); err != nil {
		return nil, err
	}
	items, err := s.store.ListRecordings(ctx, roomID)
	if err != nil {
		return nil, err
	}
	for i := range items {
		items[i].PlaybackURL = s.recordingURL(items[i].ObjectKey)
	}
	return items, nil
}

func (s *Service) RequirePublishing(ctx context.Context, roomID int64) error {
	session, err := s.store.ByRoom(ctx, roomID)
	if errors.Is(err, ErrNotConfigured) {
		// shortcut: legacy rooms may start without media until every caller provisions an SRS session.
		return nil
	}
	if err != nil {
		return err
	}
	if session.Status != StatusPublishing {
		return ErrNotPublishing
	}
	return nil
}

func (s *Service) HandleSRSCallback(ctx context.Context, callback SRSCallback) error {
	if strings.TrimSpace(callback.Stream) == "" {
		return ErrInvalidCallback
	}
	app, streamName, err := s.callbackStream(callback)
	if err != nil {
		return err
	}
	session, err := s.store.ByStream(ctx, app, streamName)
	if err != nil {
		return err
	}
	switch callback.Action {
	case "on_publish":
		values, err := url.ParseQuery(strings.TrimPrefix(callback.Param, "?"))
		if err != nil {
			return ErrInvalidPublishToken
		}
		actual := sha256.Sum256([]byte(values.Get("token")))
		if len(session.PublishHash) != sha256.Size || subtle.ConstantTimeCompare(actual[:], session.PublishHash) != 1 {
			return ErrInvalidPublishToken
		}
		return s.store.SetStatus(ctx, session.RoomID, StatusPublishing)
	case "on_unpublish":
		return s.store.SetStatus(ctx, session.RoomID, StatusDisconnected)
	case "on_play":
		v, err := s.rooms.Get(ctx, session.RoomID)
		if err != nil {
			return err
		}
		if v.Status != room.StatusLiving || session.Status != StatusPublishing {
			return ErrNotPublishing
		}
		return nil
	case "on_stop":
		return nil
	case "on_dvr":
		objectKey, err := recordingObjectKey(callback)
		if err != nil {
			return err
		}
		_, err = s.store.SaveRecording(ctx, Recording{
			RoomID: session.RoomID, Provider: session.Provider, ObjectKey: objectKey,
		})
		return err
	default:
		return fmt.Errorf("%w: unsupported action %q", ErrInvalidCallback, callback.Action)
	}
}

func (s *Service) callbackStream(callback SRSCallback) (string, string, error) {
	if callback.App == s.config.App {
		return callback.App, callback.Stream, nil
	}
	if callback.Action != "on_play" || callback.Type != "hls-play" {
		return "", "", ErrInvalidCallback
	}
	parts := strings.Split(strings.Trim(callback.App, "/"), "/")
	if len(parts) == 3 && parts[0] == "streams" && parts[1] == "origin" && parts[2] == s.config.App {
		return s.config.App, callback.Stream, nil
	}
	if len(parts) >= 4 && parts[0] == "streams" && parts[1] == "abr" && parts[2] == s.config.App {
		return s.config.App, parts[3], nil
	}
	return "", "", ErrInvalidCallback
}

func recordingObjectKey(callback SRSCallback) (string, error) {
	name := path.Base(strings.ReplaceAll(strings.TrimSpace(callback.File), "\\", "/"))
	if name == "." || name == "" || !strings.HasSuffix(strings.ToLower(name), ".mp4") {
		return "", fmt.Errorf("%w: invalid DVR file", ErrInvalidCallback)
	}
	if !strings.HasPrefix(name, callback.Stream+".") {
		return "", fmt.Errorf("%w: DVR file does not match stream", ErrInvalidCallback)
	}
	return path.Join(callback.App, name), nil
}

func (s *Service) recordingURL(objectKey string) string {
	if s.config.RecordingBaseURL == "" {
		return ""
	}
	parts := strings.Split(objectKey, "/")
	for i := range parts {
		parts[i] = url.PathEscape(parts[i])
	}
	return s.config.RecordingBaseURL + "/" + strings.Join(parts, "/")
}

func (s *Service) playbackURL(streamName string) string {
	u, err := url.Parse(s.config.WHEPBaseURL)
	if err != nil {
		return ""
	}
	query := u.Query()
	query.Set("app", s.config.App)
	query.Set("stream", streamName)
	u.RawQuery = query.Encode()
	return u.String()
}

func randomToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
