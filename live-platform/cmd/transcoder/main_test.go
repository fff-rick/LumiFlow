package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestFFmpegArgsBuildTwoVariantHLS(t *testing.T) {
	cfg := config{OriginURL: "http://srs:8080/streams/origin", OutputDir: "/data/abr"}
	args := strings.Join(ffmpegArgs(cfg, stream{App: "live", Name: "room-12"}), " ")
	for _, want := range []string{
		"http://srs:8080/streams/origin/live/room-12.m3u8",
		"v:0,a:0,name:720p v:1,a:1,name:360p",
		filepath.Join("/data/abr/live/room-12", "%v", "segment-%09d.ts"),
		"master.m3u8",
	} {
		if !strings.Contains(args, want) {
			t.Fatalf("args missing %q: %s", want, args)
		}
	}
}

func TestSafeNameRejectsPaths(t *testing.T) {
	for _, value := range []string{"live/other", "../live", "room 1", ""} {
		if safeName(value) {
			t.Fatalf("safeName(%q)=true", value)
		}
	}
	if !safeName("room-12_source") {
		t.Fatal("expected stream name to be accepted")
	}
}
