package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

type config struct {
	SRSAPIURL    string
	OriginURL    string
	OutputDir    string
	FFmpegBinary string
	App          string
	PollInterval time.Duration
}

type stream struct {
	App     string `json:"app"`
	Name    string `json:"name"`
	Publish struct {
		Active bool `json:"active"`
	} `json:"publish"`
}

type streamsResponse struct {
	Code    int      `json:"code"`
	Streams []stream `json:"streams"`
}

type job struct {
	cmd    *exec.Cmd
	cancel context.CancelFunc
}

type manager struct {
	cfg  config
	log  *slog.Logger
	mu   sync.Mutex
	jobs map[string]*job
}

func main() {
	cfg := loadConfig()
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	client := &http.Client{Timeout: 3 * time.Second}
	manager := &manager{cfg: cfg, log: log, jobs: make(map[string]*job)}
	ticker := time.NewTicker(cfg.PollInterval)
	defer ticker.Stop()
	for {
		streams, err := fetchStreams(ctx, client, cfg.SRSAPIURL)
		if err != nil && !errors.Is(err, context.Canceled) {
			log.Warn("list SRS streams", "error", err)
		} else if err == nil {
			manager.sync(ctx, streams)
		}
		select {
		case <-ctx.Done():
			manager.stopAll()
			return
		case <-ticker.C:
		}
	}
}

func loadConfig() config {
	poll, err := time.ParseDuration(env("TRANSCODER_POLL_INTERVAL", "2s"))
	if err != nil || poll <= 0 {
		poll = 2 * time.Second
	}
	return config{
		SRSAPIURL:    strings.TrimRight(env("TRANSCODER_SRS_API_URL", "http://srs:1985"), "/"),
		OriginURL:    strings.TrimRight(env("TRANSCODER_ORIGIN_URL", "http://srs:8080/streams/origin"), "/"),
		OutputDir:    env("TRANSCODER_OUTPUT_DIR", "/data/abr"),
		FFmpegBinary: env("TRANSCODER_FFMPEG_BINARY", "/usr/local/srs/objs/ffmpeg/bin/ffmpeg"),
		App:          env("MEDIA_APP", "live"),
		PollInterval: poll,
	}
}

func fetchStreams(ctx context.Context, client *http.Client, baseURL string) ([]stream, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/api/v1/streams/", nil)
	if err != nil {
		return nil, err
	}
	response, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("SRS streams status %d", response.StatusCode)
	}
	var payload streamsResponse
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return nil, err
	}
	if payload.Code != 0 {
		return nil, fmt.Errorf("SRS streams code %d", payload.Code)
	}
	return payload.Streams, nil
}

func (m *manager) sync(ctx context.Context, streams []stream) {
	active := make(map[string]stream)
	for _, value := range streams {
		if value.Publish.Active && value.App == m.cfg.App && safeName(value.App) && safeName(value.Name) {
			active[value.App+"/"+value.Name] = value
		}
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	for key, running := range m.jobs {
		if _, ok := active[key]; !ok {
			running.cancel()
			delete(m.jobs, key)
			m.log.Info("stop adaptive transcode", "stream", key)
		}
	}
	for key, value := range active {
		if _, ok := m.jobs[key]; ok {
			continue
		}
		if err := m.startLocked(ctx, key, value); err != nil {
			m.log.Error("start adaptive transcode", "stream", key, "error", err)
		}
	}
}

func (m *manager) startLocked(ctx context.Context, key string, value stream) error {
	output := filepath.Join(m.cfg.OutputDir, value.App, value.Name)
	for _, variant := range []string{"720p", "360p"} {
		if err := os.MkdirAll(filepath.Join(output, variant), 0o755); err != nil {
			return err
		}
	}
	jobCtx, cancel := context.WithCancel(ctx)
	cmd := exec.CommandContext(jobCtx, m.cfg.FFmpegBinary, ffmpegArgs(m.cfg, value)...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		cancel()
		return err
	}
	running := &job{cmd: cmd, cancel: cancel}
	m.jobs[key] = running
	m.log.Info("start adaptive transcode", "stream", key, "pid", cmd.Process.Pid)
	go func() {
		err := cmd.Wait()
		wasCanceled := jobCtx.Err() != nil
		m.mu.Lock()
		if m.jobs[key] == running {
			delete(m.jobs, key)
		}
		m.mu.Unlock()
		cancel()
		if err != nil && !wasCanceled {
			m.log.Warn("adaptive transcode exited", "stream", key, "error", err)
		}
	}()
	return nil
}

func (m *manager) stopAll() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for key, running := range m.jobs {
		running.cancel()
		delete(m.jobs, key)
	}
}

func ffmpegArgs(cfg config, value stream) []string {
	input := cfg.OriginURL + "/" + url.PathEscape(value.App) + "/" + url.PathEscape(value.Name) + ".m3u8"
	output := filepath.Join(cfg.OutputDir, value.App, value.Name)
	return []string{
		"-hide_banner", "-loglevel", "warning", "-i", input,
		"-filter_complex", "[0:v]split=2[v720in][v360in];[v720in]scale=-2:720[v720];[v360in]scale=-2:360[v360]",
		"-map", "[v720]", "-map", "0:a:0", "-c:v:0", "libx264", "-preset", "veryfast", "-profile:v:0", "main",
		"-b:v:0", "2500k", "-maxrate:v:0", "2675k", "-bufsize:v:0", "3750k", "-g:v:0", "60", "-keyint_min:v:0", "60", "-sc_threshold:v:0", "0",
		"-c:a:0", "aac", "-b:a:0", "128k", "-ac:a:0", "2",
		"-map", "[v360]", "-map", "0:a:0", "-c:v:1", "libx264", "-preset", "veryfast", "-profile:v:1", "main",
		"-b:v:1", "800k", "-maxrate:v:1", "856k", "-bufsize:v:1", "1200k", "-g:v:1", "60", "-keyint_min:v:1", "60", "-sc_threshold:v:1", "0",
		"-c:a:1", "aac", "-b:a:1", "64k", "-ac:a:1", "2",
		"-f", "hls", "-hls_time", "2", "-hls_list_size", "8", "-hls_delete_threshold", "4",
		"-hls_flags", "delete_segments+independent_segments+program_date_time", "-master_pl_name", "master.m3u8",
		"-hls_segment_filename", filepath.Join(output, "%v", "segment-%09d.ts"),
		"-var_stream_map", "v:0,a:0,name:720p v:1,a:1,name:360p", filepath.Join(output, "%v", "index.m3u8"),
	}
}

func safeName(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') && r != '-' && r != '_' {
			return false
		}
	}
	return true
}

func env(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}
