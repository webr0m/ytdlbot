package app

import (
	"context"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

// telegramVideoCompatible reports whether a video can be delivered without
// re-encoding. Telegram streams MP4 files with H.264 video and AAC audio; other
// combinations are still accepted as documents, but converting them is only
// worth the CPU/GPU cost when the user asked for a compatible video.
//
// The probe reads only the container/stream headers; it never decodes frames.
func telegramVideoCompatible(ctx context.Context, ffmpeg, path string) bool {
	probeCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(probeCtx, ffmpeg, "-hide_banner", "-nostdin", "-i", path)
	prepareProcess(cmd)
	var stderr tail
	cmd.Stderr = &stderr
	// ffmpeg returns a non-zero status when no output file is requested. That is
	// expected: the stream information was still written to stderr.
	_ = cmd.Run()
	if probeCtx.Err() != nil {
		return false
	}
	text := stderr.String()
	if !strings.Contains(text, "Input #0, mov,mp4") {
		return false
	}
	videoSeen := false
	videoOK := false
	audioSeen := false
	audioOK := true
	for _, line := range strings.Split(text, "\n") {
		if i := strings.Index(line, ": Video: "); i >= 0 {
			if strings.Contains(line, "(attached pic)") {
				continue
			}
			rest := line[i+len(": Video: "):]
			fields := strings.Fields(rest)
			if len(fields) == 0 {
				return false
			}
			if !videoSeen {
				videoSeen = true
				// yuv420p10le/yuv422p/yuv444p are not matched on purpose.
				videoOK = fields[0] == "h264" && yuv420pPattern.MatchString(rest)
			}
			continue
		}
		if i := strings.Index(line, ": Audio: "); i >= 0 {
			if audioSeen {
				continue
			}
			audioSeen = true
			rest := line[i+len(": Audio: "):]
			fields := strings.Fields(rest)
			audioOK = len(fields) > 0 && strings.HasPrefix(fields[0], "aac")
		}
	}
	return videoSeen && videoOK && audioOK
}

var yuv420pPattern = regexp.MustCompile(`\byuvj?420p\b`)

// shouldTranscodeVideo only converts when the user requested Telegram
// compatibility and the downloaded file does not already have it.
func shouldTranscodeVideo(ctx context.Context, ffmpeg, path string, o Options) bool {
	return o.TranscodeRAM && isVideo(path) && !telegramVideoCompatible(ctx, ffmpeg, path)
}
