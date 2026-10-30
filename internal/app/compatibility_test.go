package app

import (
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func makeProbeVideo(t *testing.T, ffmpeg string, args ...string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "probe.mp4")
	base := []string{"-hide_banner", "-loglevel", "error", "-y",
		"-f", "lavfi", "-i", "color=c=black:s=128x128:d=1",
		"-f", "lavfi", "-i", "sine=frequency=1000:duration=1"}
	base = append(base, args...)
	base = append(base, path)
	cmd := exec.Command(ffmpeg, base...)
	if out, e := cmd.CombinedOutput(); e != nil {
		t.Fatalf("no se pudo generar el vídeo de prueba: %v\n%s", e, out)
	}
	return path
}

func TestTelegramVideoCompatible(t *testing.T) {
	ffmpeg, e := exec.LookPath("ffmpeg")
	if e != nil {
		t.Skip("FFmpeg no disponible")
	}
	ctx := context.Background()
	compatible := makeProbeVideo(t, ffmpeg, "-c:v", "libx264", "-pix_fmt", "yuv420p", "-c:a", "aac", "-shortest")
	if !telegramVideoCompatible(ctx, ffmpeg, compatible) {
		t.Fatal("un MP4 H.264/AAC debería enviarse sin recodificar")
	}
	noAudio := makeProbeVideo(t, ffmpeg, "-c:v", "libx264", "-pix_fmt", "yuv420p", "-an")
	if !telegramVideoCompatible(ctx, ffmpeg, noAudio) {
		t.Fatal("un MP4 H.264 sin audio debería enviarse sin recodificar")
	}
	otherCodec := makeProbeVideo(t, ffmpeg, "-c:v", "mpeg4", "-c:a", "aac", "-shortest")
	if telegramVideoCompatible(ctx, ffmpeg, otherCodec) {
		t.Fatal("un códec de vídeo distinto de H.264 necesita conversión")
	}
	otherAudio := makeProbeVideo(t, ffmpeg, "-c:v", "libx264", "-pix_fmt", "yuv420p", "-c:a", "libmp3lame", "-shortest")
	if telegramVideoCompatible(ctx, ffmpeg, otherAudio) {
		t.Fatal("un audio distinto de AAC necesita conversión")
	}
	tenBit := makeProbeVideo(t, ffmpeg, "-c:v", "libx264", "-pix_fmt", "yuv420p10le", "-an")
	if telegramVideoCompatible(ctx, ffmpeg, tenBit) {
		t.Fatal("H.264 de 10 bits no es compatible con la vista previa de Telegram")
	}
}

func TestShouldTranscodeOnlyWhenNeeded(t *testing.T) {
	ffmpeg, e := exec.LookPath("ffmpeg")
	if e != nil {
		t.Skip("FFmpeg no disponible")
	}
	ctx := context.Background()
	compatible := makeProbeVideo(t, ffmpeg, "-c:v", "libx264", "-pix_fmt", "yuv420p", "-c:a", "aac", "-shortest")
	incompatible := makeProbeVideo(t, ffmpeg, "-c:v", "mpeg4", "-c:a", "aac", "-shortest")
	if shouldTranscodeVideo(ctx, ffmpeg, compatible, Options{TranscodeRAM: true}) {
		t.Fatal("no se debe recodificar un archivo ya compatible")
	}
	if !shouldTranscodeVideo(ctx, ffmpeg, incompatible, Options{TranscodeRAM: true}) {
		t.Fatal("un archivo incompatible debe convertirse cuando la opción está activa")
	}
	if shouldTranscodeVideo(ctx, ffmpeg, incompatible, Options{TranscodeRAM: false}) {
		t.Fatal("sin la opción activa se debe conservar el archivo original")
	}
}

func TestVideoFormatPrefersH264AAC(t *testing.T) {
	format := videoFormat(1080)
	for _, part := range []string{"vcodec^=avc", "acodec^=mp4a", "height<=1080"} {
		if !strings.Contains(format, part) {
			t.Fatalf("el selector no contiene %q: %s", part, format)
		}
	}
}
