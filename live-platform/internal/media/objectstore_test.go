package media

import (
	"bytes"
	"context"
	"io"
	"testing"
	"time"
)

func TestRateLimitedReaderCapsUploadBody(t *testing.T) {
	body := bytes.NewReader(make([]byte, 50_000))
	reader := &rateLimitedReader{
		ctx: context.Background(), reader: body, bytesPerSecond: 500_000, started: time.Now(),
	}
	started := time.Now()
	if _, err := io.Copy(io.Discard, reader); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(started); elapsed < 80*time.Millisecond {
		t.Fatalf("upload body was not rate limited: %s", elapsed)
	}
}
