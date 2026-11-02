package media

import "time"

type Status string

const (
	StatusCreated      Status = "CREATED"
	StatusPublishing   Status = "PUBLISHING"
	StatusDisconnected Status = "DISCONNECTED"
)

type Session struct {
	RoomID         int64      `json:"room_id"`
	Provider       string     `json:"provider"`
	App            string     `json:"app"`
	StreamName     string     `json:"stream_name"`
	PublishHash    []byte     `json:"-"`
	Status         Status     `json:"status"`
	PublishedAt    *time.Time `json:"published_at,omitempty"`
	DisconnectedAt *time.Time `json:"disconnected_at,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

type Credentials struct {
	RoomID      int64  `json:"room_id"`
	Provider    string `json:"provider"`
	Status      Status `json:"status"`
	ServerURL   string `json:"server_url"`
	StreamKey   string `json:"stream_key"`
	PlaybackURL string `json:"whep_url"`
}

type Playback struct {
	RoomID      int64  `json:"room_id"`
	Provider    string `json:"provider,omitempty"`
	Status      Status `json:"status,omitempty"`
	PlaybackURL string `json:"whep_url,omitempty"`
	HLSURL      string `json:"hls_url,omitempty"`
}

type Recording struct {
	ID              int64      `json:"recording_id"`
	RoomID          int64      `json:"room_id"`
	Provider        string     `json:"provider"`
	ObjectKey       string     `json:"-"`
	StorageStatus   string     `json:"storage_status"`
	SizeBytes       int64      `json:"size_bytes,omitempty"`
	ArchiveAttempts int        `json:"-"`
	ArchiveStarted  *time.Time `json:"-"`
	ArchivedAt      *time.Time `json:"archived_at,omitempty"`
	PlaybackURL     string     `json:"playback_url"`
	CreatedAt       time.Time  `json:"created_at"`
}

const (
	RecordingLocal     = "LOCAL"
	RecordingPending   = "PENDING"
	RecordingUploading = "UPLOADING"
	RecordingFailed    = "FAILED"
	RecordingArchived  = "ARCHIVED"
)

type SRSCallback struct {
	Action   string `json:"action"`
	Type     string `json:"type"`
	ClientID string `json:"client_id"`
	IP       string `json:"ip"`
	VHost    string `json:"vhost"`
	App      string `json:"app"`
	Stream   string `json:"stream"`
	Param    string `json:"param"`
	File     string `json:"file"`
}
