package media

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type RecordingArchiveStore interface {
	ClaimRecording(context.Context, time.Duration) (Recording, error)
	MarkRecordingArchived(context.Context, int64, int64) error
	MarkRecordingFailed(context.Context, int64, time.Time, string) error
}

type RecordingObjectStore interface {
	Put(context.Context, string, io.Reader, int64) error
}

type ArchiverConfig struct {
	RecordingDir string
	PollInterval time.Duration
	Lease        time.Duration
}

type Archiver struct {
	log     *slog.Logger
	repo    RecordingArchiveStore
	objects RecordingObjectStore
	config  ArchiverConfig
}

func NewArchiver(log *slog.Logger, repo RecordingArchiveStore, objects RecordingObjectStore, config ArchiverConfig) *Archiver {
	return &Archiver{log: log, repo: repo, objects: objects, config: config}
}

func (a *Archiver) Run(ctx context.Context) error {
	for {
		err := a.archiveOne(ctx)
		if err != nil && !errors.Is(err, sql.ErrNoRows) && !errors.Is(err, context.Canceled) {
			a.log.Warn("archive recording", "error", err)
		}
		if err == nil {
			continue
		}
		timer := time.NewTimer(a.config.PollInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func (a *Archiver) archiveOne(ctx context.Context) error {
	recording, err := a.repo.ClaimRecording(ctx, a.config.Lease)
	if err != nil {
		return err
	}
	fail := func(cause error) error {
		message := cause.Error()
		if len(message) > 512 {
			message = message[:512]
		}
		retryAt := time.Now().UTC().Add(recordingRetryDelay(recording.ArchiveAttempts))
		if markErr := a.repo.MarkRecordingFailed(ctx, recording.ID, retryAt, message); markErr != nil {
			return errors.Join(cause, fmt.Errorf("mark recording failed: %w", markErr))
		}
		return cause
	}

	filename, err := recordingFilePath(a.config.RecordingDir, recording.ObjectKey)
	if err != nil {
		return fail(err)
	}
	file, err := os.Open(filename)
	if err != nil {
		return fail(err)
	}
	defer func() { _ = file.Close() }()
	info, err := file.Stat()
	if err != nil {
		return fail(err)
	}
	if !info.Mode().IsRegular() || info.Size() <= 0 {
		return fail(fmt.Errorf("recording is not a non-empty regular file"))
	}

	if err := a.objects.Put(ctx, recording.ObjectKey, file, info.Size()); err != nil {
		return fail(err)
	}
	if err := a.repo.MarkRecordingArchived(ctx, recording.ID, info.Size()); err != nil {
		return fail(err)
	}
	a.log.Info("recording archived", "recording_id", recording.ID, "object_key", recording.ObjectKey, "size_bytes", info.Size())
	return nil
}

func recordingFilePath(root, objectKey string) (string, error) {
	cleanKey := filepath.Clean(filepath.FromSlash(objectKey))
	if cleanKey == "." || filepath.IsAbs(cleanKey) || cleanKey == ".." || strings.HasPrefix(cleanKey, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("invalid recording object key %q", objectKey)
	}
	root = filepath.Clean(root)
	filename := filepath.Join(root, cleanKey)
	if relative, err := filepath.Rel(root, filename); err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("recording path escapes root")
	}
	return filename, nil
}

func recordingRetryDelay(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	if attempt > 7 {
		attempt = 7
	}
	delay := 30 * time.Second * time.Duration(1<<(attempt-1))
	if delay > 30*time.Minute {
		return 30 * time.Minute
	}
	return delay
}
