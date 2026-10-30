package app

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

func (s *Store) Stage(ctx context.Context, j Job, stage, detail string, progress float64) {
	if s == nil {
		return
	}
	s.Pool.Exec(ctx, "UPDATE jobs SET stage=$3,detail=$4,progress=$5,updated_at=now() WHERE id=$1 AND worker=$2 AND state IN ('downloading','uploading')", j.ID, j.Worker, stage, detail, progress)
}
func (s *Store) Delivered(ctx context.Context, j Job, out Output) {
	if s == nil {
		return
	}
	b, _ := json.Marshal([]Output{out})
	s.Pool.Exec(ctx, "UPDATE jobs SET files=files || $3::jsonb,updated_at=now() WHERE id=$1 AND worker=$2 AND state='uploading'", j.ID, j.Worker, b)
}

type uploadCounter struct {
	source   io.Reader
	n, total int64
	last     time.Time
	update   func(int64, int64)
}

func (c *uploadCounter) Read(p []byte) (int, error) {
	n, e := c.source.Read(p)
	c.n += int64(n)
	if time.Since(c.last) > time.Second || c.n == c.total {
		c.update(c.n, c.total)
		c.last = time.Now()
	}
	return n, e
}

var durationPattern = regexp.MustCompile(`Duration: (\d+):(\d+):(\d+(?:\.\d+)?)`)

func mediaDuration(ctx context.Context, ffmpeg, path string) float64 {
	cmd := exec.CommandContext(ctx, ffmpeg, "-hide_banner", "-nostdin", "-i", path, "-t", "0", "-f", "null", "-")
	prepareProcess(cmd)
	var stderr tail
	cmd.Stderr = &stderr
	cmd.Run()
	m := durationPattern.FindStringSubmatch(stderr.String())
	if len(m) != 4 {
		return 0
	}
	h, _ := strconv.ParseFloat(m[1], 64)
	min, _ := strconv.ParseFloat(m[2], 64)
	sec, _ := strconv.ParseFloat(m[3], 64)
	return h*3600 + min*60 + sec
}

var encoderCache sync.Map

func selectEncoder(ctx context.Context, ffmpeg, setting string) (string, string) {
	if setting == "libx264" {
		return "libx264", "CPU"
	}
	if v, ok := encoderCache.Load(ffmpeg + setting); ok {
		e := v.(string)
		return e, encoderName(e)
	}
	candidates := []string{"h264_amf", "h264_nvenc", "h264_qsv"}
	if setting != "" && setting != "auto" {
		candidates = []string{setting}
	}
	selected := "libx264"
	for _, enc := range candidates {
		probeCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
		cmd := exec.CommandContext(probeCtx, ffmpeg, "-hide_banner", "-loglevel", "error", "-f", "lavfi", "-i", "color=s=128x128:d=0.1", "-frames:v", "1", "-c:v", enc, "-f", "null", "-")
		prepareProcess(cmd)
		e := cmd.Run()
		cancel()
		if e == nil {
			selected = enc
			break
		}
	}
	encoderCache.Store(ffmpeg+setting, selected)
	return selected, encoderName(selected)
}
func encoderName(e string) string {
	switch e {
	case "h264_amf":
		return "AMD GPU"
	case "h264_nvenc":
		return "NVIDIA GPU"
	case "h264_qsv":
		return "Intel GPU"
	}
	return "CPU"
}
func encoderArgs(enc string) []string {
	switch enc {
	case "h264_amf":
		return []string{"-c:v", enc, "-quality", "speed", "-rc", "cqp", "-qp_i", "23", "-qp_p", "23"}
	case "h264_nvenc":
		return []string{"-c:v", enc, "-preset", "p4", "-rc", "vbr", "-cq", "23", "-b:v", "0"}
	case "h264_qsv":
		return []string{"-c:v", enc, "-preset", "veryfast", "-global_quality", "23"}
	}
	return []string{"-c:v", "libx264", "-preset", "veryfast", "-crf", "23", "-pix_fmt", "yuv420p"}
}
func hardwareInput(enc string) []string {
	if runtime.GOOS == "windows" && enc == "h264_amf" {
		return []string{"-hwaccel", "d3d11va", "-hwaccel_output_format", "d3d11"}
	}
	return nil
}

// Probe the actual source, because GPU decoding support varies by codec,
// profile and frame dimensions. A failed hardware path falls back safely.
func sourceEncoder(ctx context.Context, ffmpeg, path, enc string) (string, []string) {
	if enc == "libx264" {
		return enc, nil
	}
	inputs := [][]string{hardwareInput(enc), nil}
	for _, input := range inputs {
		probeCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
		args := []string{"-hide_banner", "-loglevel", "error", "-nostdin"}
		args = append(args, input...)
		args = append(args, "-i", path, "-t", "0.12", "-map", "0:v:0", "-an")
		args = append(args, encoderArgs(enc)...)
		args = append(args, "-f", "null", "-")
		cmd := exec.CommandContext(probeCtx, ffmpeg, args...)
		prepareProcess(cmd)
		e := cmd.Run()
		cancel()
		if e == nil {
			return enc, input
		}
	}
	return "libx264", nil
}

type encodeProgress struct {
	buf      string
	last     time.Time
	duration float64
	label    string
	ctx      context.Context
	s        *Store
	j        Job
}

func (p *encodeProgress) Write(b []byte) (int, error) {
	p.buf += string(b)
	for {
		line, rest, ok := strings.Cut(p.buf, "\n")
		if !ok {
			break
		}
		p.buf = rest
		if strings.HasPrefix(line, "out_time_us=") && time.Since(p.last) > time.Second {
			n, _ := strconv.ParseFloat(strings.TrimPrefix(line, "out_time_us="), 64)
			seconds := n / 1e6
			percent := float64(0)
			if p.duration > 0 {
				percent = seconds * 100 / p.duration
			}
			p.s.Stage(p.ctx, p.j, "transcoding", fmt.Sprintf("Convirtiendo con %s · %.1f%% · %.0f / %.0f min", p.label, percent, seconds/60, p.duration/60), percent)
			p.last = time.Now()
		}
	}
	if len(p.buf) > 8192 {
		p.buf = p.buf[len(p.buf)-8192:]
	}
	return len(b), nil
}
