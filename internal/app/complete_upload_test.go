package app

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"webrom/ytdlbot/internal/telegram"
	"webrom/ytdlbot/internal/virtualfile"
)

func TestVirtualPartsAreOneTelegramFile(t *testing.T) {
	payload := bytes.Repeat([]byte{0, 1, 254, 99}, 1000)
	ctx := context.Background()
	manifest, e := virtualfile.Stage(ctx, bytes.NewReader(payload), t.TempDir(), "original.bin", 321, 10000)
	if e != nil {
		t.Fatal(e)
	}
	calls := 0
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if e := r.ParseMultipartForm(10000); e != nil {
			t.Error(e)
			return
		}
		defer r.MultipartForm.RemoveAll()
		f, h, e := r.FormFile("document")
		if e != nil {
			t.Error(e)
			return
		}
		defer f.Close()
		got, _ := io.ReadAll(f)
		if h.Filename != "original.bin" || !bytes.Equal(got, payload) {
			t.Error("received parts instead of original file")
		}
		io.WriteString(w, `{"ok":true,"result":{"document":{"file_id":"whole"}}}`)
	}))
	defer mock.Close()
	s := NewServer(Config{MaxBytes: 10000}, nil, nil)
	s.Bot = telegram.New(mock.URL, "mock")
	out, e := s.sendVirtualManifest(ctx, Job{ChatID: 123}, manifest)
	if e != nil {
		t.Fatal(e)
	}
	if calls != 1 || len(out) != 1 || out[0].Name != "original.bin" || out[0].Size != int64(len(payload)) {
		t.Fatal("not one complete output")
	}
}
func TestCompleteFileLimitAndOwnerIsolation(t *testing.T) {
	s := NewServer(Config{MaxBytes: 100, SavedUploads: true, MTProto: "must-not-run", Admins: map[int64]bool{999: true}}, nil, nil)
	for _, j := range []Job{{UserID: 123, ChatID: 123}, {UserID: 999, ChatID: 123}} {
		if s.canSave(j) || s.completeLimit(j) != 100 {
			t.Fatal("private session exposed")
		}
		if _, e := s.sendComplete(context.Background(), j, "out.bin", 101, strings.NewReader(""), "", ""); e == nil {
			t.Fatal("allowed oversized bot upload")
		}
	}
	if !s.canSave(Job{UserID: 999, ChatID: 999}) || s.completeLimit(Job{UserID: 999, ChatID: 999}) != 4*1024*1024*1024 {
		t.Fatal("owner premium transport unavailable")
	}
}
func TestFailedTranscodeNeverUploads(t *testing.T) {
	t.Setenv("FFMPEG_ENCODER", "libx264")
	ff, e := exec.LookPath("ffmpeg")
	if e != nil {
		t.Skip("FFmpeg unavailable")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "broken.mp4")
	os.WriteFile(path, []byte("not a video"), 0600)
	s := NewServer(Config{FFmpeg: ff, FFmpegThreads: 1, MaxBytes: 1000000, RAMPartMB: 1}, nil, nil)
	if out, e := s.sendSplit(context.Background(), Job{}, path, true); e == nil || len(out) > 0 {
		t.Fatal("accepted incomplete encoder output")
	}
	staged, _ := filepath.Glob(filepath.Join(dir, ".delivery-*"))
	if len(staged) != 0 {
		t.Fatal("failed output blocks leaked")
	}
}
