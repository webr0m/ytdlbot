package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"webrom/ytdlbot/internal/virtualfile"
)

type Part struct {
	Name   string `json:"name"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}
type Manifest struct {
	Version  int    `json:"version"`
	Original string `json:"original"`
	Size     int64  `json:"size"`
	SHA256   string `json:"sha256"`
	Parts    []Part `json:"parts"`
	Note     string `json:"note"`
}

// SendParts uses one reusable RAM buffer and backpressure: encoding/download
// blocks while Telegram uploads, so outstanding memory never grows with file size.
func SendParts(ctx context.Context, source io.Reader, name string, partBytes int, send func(string, io.Reader) (string, error)) ([]Output, Manifest, error) {
	m := Manifest{Version: 1, Original: filepath.Base(name), Parts: []Part{}, Note: "Partes binarias. Reconstruir con scripts/join-parts.mjs y verificar SHA-256 antes de abrir."}
	outputs := []Output{}
	if partBytes < 1 {
		return outputs, m, fmt.Errorf("tamaño de parte inválido")
	}
	buf := make([]byte, partBytes)
	fullHash := sha256.New()
	for index := 1; ; index++ {
		if er := ctx.Err(); er != nil {
			return outputs, m, er
		}
		limit := len(buf)
		if index == 1 && limit > 32*1024*1024 {
			limit = 32 * 1024 * 1024
		}
		n, er := io.ReadFull(source, buf[:limit])
		if er != nil && er != io.EOF && er != io.ErrUnexpectedEOF {
			return outputs, m, er
		}
		if n == 0 {
			break
		}
		payload := buf[:n]
		hash := sha256.Sum256(payload)
		partName := fmt.Sprintf("%s.part%05d", filepath.Base(name), index)
		id, uploadErr := send(partName, bytes.NewReader(payload))
		if uploadErr != nil {
			return outputs, m, uploadErr
		}
		fullHash.Write(payload)
		m.Size += int64(n)
		m.Parts = append(m.Parts, Part{partName, int64(n), hex.EncodeToString(hash[:])})
		outputs = append(outputs, Output{Name: partName, Size: int64(n), FileID: id})
		if er != nil {
			break
		}
	}
	m.SHA256 = hex.EncodeToString(fullHash.Sum(nil))
	if m.Size == 0 {
		return outputs, m, fmt.Errorf("archivo vacío")
	}
	return outputs, m, nil
}

// sendSplit now keeps processing blocks internal and sends exactly one final file.
func (s *Server) sendSplit(ctx context.Context, j Job, path string, ram bool) ([]Output, error) {
	name := filepath.Base(path)
	if !ram {
		f, e := os.Open(path)
		if e != nil {
			return nil, e
		}
		defer f.Close()
		info, e := f.Stat()
		if e != nil {
			return nil, e
		}
		return s.sendComplete(ctx, j, name, info.Size(), f, path, "")
	}
	name = strings.TrimSuffix(name, filepath.Ext(name)) + ".compatible.mp4"
	duration := mediaDuration(ctx, s.Config.FFmpeg, path)
	encoder, _ := selectEncoder(ctx, s.Config.FFmpeg, env("FFMPEG_ENCODER", "auto"))
	encoder, inputArgs := sourceEncoder(ctx, s.Config.FFmpeg, path, encoder)
	label := encoderName(encoder)
	s.Store.Stage(ctx, j, "transcoding", "Convirtiendo con "+label+"; preparando un único archivo", 0)
	a := []string{"-hide_banner", "-loglevel", "error", "-nostdin", "-progress", "pipe:2", "-stats_period", "1", "-protocol_whitelist", "file,pipe,crypto,data", "-threads", strconv.Itoa(s.Config.FFmpegThreads)}
	a = append(a, inputArgs...)
	a = append(a, "-i", path, "-map", "0:v:0", "-map", "0:a:0?")
	a = append(a, encoderArgs(encoder)...)
	a = append(a, "-threads", strconv.Itoa(s.Config.FFmpegThreads), "-filter_threads", "1", "-c:a", "aac", "-b:a", "160k", "-movflags", "frag_keyframe+empty_moov+default_base_moof", "-f", "mp4", "pipe:1")
	cmd := exec.CommandContext(ctx, s.Config.FFmpeg, a...)
	prepareProcess(cmd)
	source, e := cmd.StdoutPipe()
	if e != nil {
		return nil, e
	}
	defer source.Close()
	var stderr tail
	cmd.Stderr = io.MultiWriter(&stderr, &encodeProgress{duration: duration, label: label, ctx: ctx, s: s.Store, j: j})
	// A task-local staging directory bounds memory and allows size/hash/encoder
	// validation before sending. Parts are removed after confirmation or failure.
	dir, e := os.MkdirTemp(filepath.Dir(path), ".delivery-")
	if e != nil {
		return nil, e
	}
	defer os.RemoveAll(dir)
	if e = cmd.Start(); e != nil {
		return nil, fmt.Errorf("FFmpeg no disponible")
	}
	manifest, e := virtualfile.Stage(ctx, source, dir, name, int64(s.Config.RAMPartMB)*1000000, s.completeLimit(j))
	if e != nil {
		cmd.Cancel()
	}
	wait := cmd.Wait()
	if e != nil {
		return nil, e
	}
	if wait != nil {
		return nil, fmt.Errorf("FFmpeg no pudo completar el vídeo; no se envió un archivo incompleto")
	}
	return s.sendVirtualManifest(ctx, j, manifest)
}

func (s *Server) sendVirtualManifest(ctx context.Context, j Job, manifest string) ([]Output, error) {
	s.Store.Stage(ctx, j, "joining", "Verificando y uniendo virtualmente los bloques", 0)
	v, e := virtualfile.Open(ctx, manifest)
	if e != nil {
		return nil, e
	}
	r := v.Reader(ctx)
	defer r.Close()
	return s.sendComplete(ctx, j, v.Manifest.Original, v.Manifest.Size, r, "", manifest)
}
