package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

func (s *Server) canSave(j Job) bool {
	return s.Config.SavedUploads && s.Config.MTProto != "" && s.Config.Admins[j.UserID] && j.UserID == j.ChatID
}
func (s *Server) completeLimit(j Job) int64 {
	if s.canSave(j) {
		return 4 * 1024 * 1024 * 1024
	}
	return s.Config.MaxBytes
}

func (s *Server) sendComplete(ctx context.Context, j Job, name string, size int64, source io.Reader, path, manifest string) ([]Output, error) {
	if size <= 0 || size > s.completeLimit(j) {
		return nil, fmt.Errorf("el archivo unido supera el máximo de %.0f MB; reduce la calidad o recorta el vídeo", float64(s.completeLimit(j))/1e6)
	}
	if size > s.Config.MaxBytes {
		return s.sendSaved(ctx, j, name, size, path, manifest)
	}
	if s.Bot == nil {
		return nil, fmt.Errorf("Telegram no configurado")
	}
	kind := "document"
	if !j.Options.AsDocument {
		switch strings.ToLower(filepath.Ext(name)) {
		case ".mp4":
			kind = "video"
		case ".mp3", ".m4a":
			kind = "audio"
		}
	}
	s.Store.Stage(ctx, j, "uploading", "Enviando archivo completo a Telegram", 0)
	counter := &uploadCounter{source: source, total: size, update: func(done, total int64) {
		pct := float64(done) * 100 / float64(total)
		detail := fmt.Sprintf("Enviando archivo completo · %.1f / %.1f MB (%.0f%%)", float64(done)/1e6, float64(total)/1e6, pct)
		if done == total {
			detail = "Archivo transferido; esperando confirmación de Telegram"
		}
		s.Store.Stage(ctx, j, "uploading", detail, pct)
	}}
	id, e := s.uploadSlot(ctx, func() (string, error) {
		return s.Bot.UploadReader(ctx, j.ChatID, name, kind, counter)
	})
	if e != nil {
		return nil, e
	}
	out := Output{Name: name, Size: size, FileID: id, Kind: kind}
	s.Store.Delivered(ctx, j, out)
	return []Output{out}, nil
}

type savedProgress struct {
	pending string
	ctx     context.Context
	s       *Store
	j       Job
}

func (p *savedProgress) Write(b []byte) (int, error) {
	p.pending += string(b)
	for {
		line, rest, ok := strings.Cut(p.pending, "\n")
		if !ok {
			break
		}
		p.pending = rest
		var state struct{ Uploaded, Total int64 }
		if json.Unmarshal([]byte(line), &state) == nil && state.Total > 0 {
			pct := float64(state.Uploaded) * 100 / float64(state.Total)
			p.s.Stage(p.ctx, p.j, "uploading", fmt.Sprintf("Enviando archivo completo a Mensajes guardados · %.1f / %.1f MB (%.0f%%)", float64(state.Uploaded)/1e6, float64(state.Total)/1e6, pct), pct)
		}
	}
	if len(p.pending) > 8192 {
		p.pending = ""
	}
	return len(b), nil
}
func (s *Server) sendSaved(ctx context.Context, j Job, name string, size int64, path, manifest string) ([]Output, error) {
	if !s.canSave(j) {
		return nil, fmt.Errorf("Mensajes guardados solo está disponible para el propietario de la sesión")
	}
	args := []string{"-owner", strconv.FormatInt(j.UserID, 10)}
	if manifest != "" {
		args = append(args, "-upload-manifest", manifest)
	} else {
		args = append(args, "-upload", path)
	}
	s.Store.Stage(ctx, j, "uploading", "Conectando con Mensajes guardados del propietario", 0)
	cmd := exec.CommandContext(ctx, s.Config.MTProto, args...)
	prepareProcess(cmd)
	var result bytes.Buffer
	var diagnostic tail
	cmd.Stdout = &result
	cmd.Stderr = io.MultiWriter(&diagnostic, &savedProgress{ctx: ctx, s: s.Store, j: j})
	if e := s.uploadSlotErr(ctx, func() error { return cmd.Run() }); e != nil {
		return nil, fmt.Errorf("MTProto no confirmó la entrega; revisa Mensajes guardados antes de reintentar y comprueba que la sesión siga siendo Premium")
	}
	var sent struct {
		SavedMessageID int    `json:"savedMessageId"`
		Name           string `json:"name"`
		Size           int64  `json:"size"`
		Verified       bool   `json:"verified"`
	}
	if json.Unmarshal(result.Bytes(), &sent) != nil || !sent.Verified || sent.SavedMessageID <= 0 || sent.Size != size || sent.Name != name {
		return nil, fmt.Errorf("confirmación MTProto inválida; revisa Mensajes guardados antes de reintentar")
	}
	out := Output{Name: name, Size: size, Kind: "saved", SavedMessageID: sent.SavedMessageID}
	s.Store.Delivered(ctx, j, out)
	return []Output{out}, nil
}
