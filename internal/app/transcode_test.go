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
	"testing"
	"time"
	"webrom/ytdlbot/internal/telegram"
)

func TestTranscodeRAMThroughMultipart(t *testing.T) {
	t.Setenv("FFMPEG_ENCODER", "libx264")
	ff, e := exec.LookPath("ffmpeg")
	if e != nil {
		t.Skip("FFmpeg no disponible")
	}
	dir := t.TempDir()
	source := filepath.Join(dir, "input.mp4")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, ff, "-hide_banner", "-loglevel", "error", "-f", "lavfi", "-i", "color=c=blue:s=160x120:d=1", "-c:v", "libx264", "-threads", "1", source)
	if e = cmd.Run(); e != nil {
		t.Fatal(e)
	}
	var reconstructed bytes.Buffer
	count := 0
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if e := r.ParseMultipartForm(4 << 20); e != nil {
			t.Error(e)
			http.Error(w, "bad", 400)
			return
		}
		defer r.MultipartForm.RemoveAll()
		f, h, e := r.FormFile("video")
		if e != nil {
			t.Error(e)
			return
		}
		defer f.Close()
		b, _ := io.ReadAll(f)
		if h.Filename != "input.compatible.mp4" || r.FormValue("supports_streaming") != "true" {
			t.Error("expected one complete streaming video")
		}
		reconstructed.Write(b)
		count++
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"ok":true,"result":{"video":{"file_id":"mock"}}}`)
	}))
	defer mock.Close()
	s := NewServer(Config{FFmpeg: ff, FFmpegThreads: 1, MaxBytes: 1900000000, RAMPartMB: 1}, nil, nil)
	s.Bot = telegram.New(mock.URL, "mock")
	out, e := s.sendSplit(ctx, Job{ChatID: 123}, source, true)
	if e != nil {
		t.Fatal(e)
	}
	if count != 1 || len(out) != 1 || out[0].Size != int64(reconstructed.Len()) || out[0].Kind != "video" {
		t.Fatal("salida incompleta")
	}
	if !bytes.Contains(reconstructed.Bytes(), []byte("ftyp")) {
		t.Fatal("no generó MP4")
	}
	output := filepath.Join(dir, "output.mp4")
	if e = os.WriteFile(output, reconstructed.Bytes(), 0600); e != nil {
		t.Fatal(e)
	}
	if b, e := exec.CommandContext(ctx, ff, "-v", "error", "-i", output, "-f", "null", "-").CombinedOutput(); e != nil {
		t.Fatalf("final video cannot decode: %v %s", e, b)
	}
	staged, _ := filepath.Glob(filepath.Join(dir, ".delivery-*"))
	if len(staged) != 0 {
		t.Fatal("internal staging blocks leaked")
	}

}
