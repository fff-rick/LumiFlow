package media

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type fakeArchiveRepo struct {
	recording Recording
	archived  bool
	failed    bool
}

func (f *fakeArchiveRepo) ClaimRecording(context.Context, time.Duration) (Recording, error) {
	f.recording.ArchiveAttempts++
	return f.recording, nil
}
func (f *fakeArchiveRepo) MarkRecordingArchived(_ context.Context, id, size int64) error {
	f.archived = id == f.recording.ID && size > 0
	return nil
}
func (f *fakeArchiveRepo) MarkRecordingFailed(context.Context, int64, time.Time, string) error {
	f.failed = true
	return nil
}

type fakeObjects struct {
	key  string
	body []byte
	err  error
}

func (f *fakeObjects) Put(_ context.Context, key string, reader io.Reader, size int64) error {
	if f.err != nil {
		return f.err
	}
	f.key = key
	f.body, _ = io.ReadAll(reader)
	if int64(len(f.body)) != size {
		return io.ErrUnexpectedEOF
	}
	return nil
}

func TestArchiverPersistsUploadFailure(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "live"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "live", "failed.mp4"), []byte("video"), 0o644); err != nil {
		t.Fatal(err)
	}
	repo := &fakeArchiveRepo{recording: Recording{ID: 8, ObjectKey: "live/failed.mp4"}}
	archiver := NewArchiver(slog.Default(), repo, &fakeObjects{err: io.ErrClosedPipe}, ArchiverConfig{
		RecordingDir: root, Lease: time.Minute,
	})
	if err := archiver.archiveOne(context.Background()); err == nil {
		t.Fatal("expected archive failure")
	}
	if !repo.failed || repo.archived {
		t.Fatalf("repo=%+v", repo)
	}
}

func TestArchiverUploadsAndMarksRecording(t *testing.T) {
	root := t.TempDir()
	key := "live/room-12.1.mp4"
	if err := os.MkdirAll(filepath.Join(root, "live"), 0o755); err != nil {
		t.Fatal(err)
	}
	want := []byte("mp4-recording")
	if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(key)), want, 0o644); err != nil {
		t.Fatal(err)
	}
	repo := &fakeArchiveRepo{recording: Recording{ID: 7, ObjectKey: key}}
	objects := &fakeObjects{}
	archiver := NewArchiver(slog.Default(), repo, objects, ArchiverConfig{RecordingDir: root, Lease: time.Minute})
	if err := archiver.archiveOne(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !repo.archived || repo.failed || objects.key != key || !bytes.Equal(objects.body, want) {
		t.Fatalf("repo=%+v objects=%+v", repo, objects)
	}
}

func TestRecordingFilePathRejectsTraversal(t *testing.T) {
	for _, key := range []string{"../secret.mp4", "/tmp/secret.mp4", "live/../../secret.mp4"} {
		if _, err := recordingFilePath("/recordings", key); err == nil {
			t.Fatalf("key %q was accepted", key)
		}
	}
}

func TestRecordingRetryDelayIsCapped(t *testing.T) {
	if got := recordingRetryDelay(1); got != 30*time.Second {
		t.Fatalf("first retry=%s", got)
	}
	if got := recordingRetryDelay(99); got != 30*time.Minute {
		t.Fatalf("capped retry=%s", got)
	}
}
