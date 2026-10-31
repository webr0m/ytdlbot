package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
)

var jobIDPattern = regexp.MustCompile(`^[a-f0-9]{32}$`)

func (s *Server) Workers(ctx context.Context, wg *sync.WaitGroup) {
	var saved int
	if s.Store.Pool.QueryRow(ctx, "SELECT value FROM bot_state WHERE key='concurrency'").Scan(&saved) == nil && saved >= 1 && saved <= maxConcurrentJobs {
		s.concurrency.Store(int32(saved))
	}
	for i := 0; i < maxConcurrentJobs; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			owner := newID()
			tick := time.NewTicker(2 * time.Second)
			defer tick.Stop()
			for {
				if ctx.Err() != nil {
					return
				}
				if s.tryWork(ctx, owner) {
					continue
				}
				select {
				case <-ctx.Done():
					return
				case <-s.wake:
				case <-tick.C:
				}
			}
		}()
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		s.maintenance(ctx)
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.maintenance(ctx)
			}
		}
	}()
}
func (s *Server) tryWork(ctx context.Context, owner string) bool {
	if !s.acquireSlot() {
		return false
	}
	defer s.activeSlots.Add(-1)
	var paused bool
	if e := s.Store.Pool.QueryRow(ctx, "SELECT COALESCE((SELECT value=1 FROM bot_state WHERE key='paused'),false)").Scan(&paused); e != nil || paused {
		return false
	}
	if !s.hasDiskFree() {
		return false
	}
	j, e := s.Store.Claim(ctx, owner)
	if errors.Is(e, pgx.ErrNoRows) {
		return false
	}
	if e != nil {
		if ctx.Err() == nil {
			slog.Warn("no se pudo tomar tarea", "error", e)
		}
		return false
	}
	s.safeRunJob(ctx, j)
	s.signalQueue()
	return true
}

func (s *Server) safeRunJob(parent context.Context, j Job) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("tarea interrumpida por un error interno", "id", j.ID, "panic", r)
			finishCtx, done := context.WithTimeout(context.Background(), 10*time.Second)
			defer done()
			s.Store.Finish(finishCtx, j, "failed", "error interno del servidor", nil)
			if jobIDPattern.MatchString(j.ID) {
				os.RemoveAll(filepath.Join(s.Config.DataDir, "jobs", j.ID))
			}
		}
	}()
	s.runJob(parent, j)
}

func (s *Server) runJob(parent context.Context, j Job) {
	ctx, cancel := context.WithTimeout(parent, s.Config.Timeout)
	defer cancel()
	dir := filepath.Join(s.Config.DataDir, "jobs", j.ID)
	if er := os.MkdirAll(dir, 0700); er != nil {
		s.Store.Finish(parent, j, "failed", "no se pudo crear el directorio", nil)
		return
	}
	var reason string
	var reasonMu sync.Mutex
	watchDone := make(chan struct{})
	go func() {
		defer close(watchDone)
		stateTick := time.NewTicker(2 * time.Second)
		heartbeat := time.NewTicker(15 * time.Second)
		defer stateTick.Stop()
		defer heartbeat.Stop()
		failures := 0
		for {
			select {
			case <-ctx.Done():
				return
			case <-stateTick.C:
				var state string
				er := s.Store.Pool.QueryRow(ctx, "SELECT state FROM jobs WHERE id=$1 AND worker=$2", j.ID, j.Worker).Scan(&state)
				if er != nil {
					// A transient database blip must not destroy an active
					// download. Only give up after several consecutive errors.
					failures++
					if failures >= 5 {
						cancel()
						return
					}
					continue
				}
				failures = 0
				if state == "cancelled" {
					cancel()
					return
				}
				if !s.hasDiskFree() {
					reasonMu.Lock()
					reason = "espacio libre insuficiente: se detuvo para proteger el equipo"
					reasonMu.Unlock()
					cancel()
					return
				}
			case <-heartbeat.C:
				s.Store.Pool.Exec(ctx, "UPDATE jobs SET updated_at=now() WHERE id=$1 AND worker=$2 AND state IN ('downloading','uploading')", j.ID, j.Worker)
			}
		}
	}()
	var last time.Time
	er := s.Engine.Download(ctx, j, dir, func(n float64) {
		if time.Since(last) > time.Second {
			if n < 0 {
				n = 0
			}
			if n > 100 {
				n = 100
			}
			s.Store.Progress(ctx, j.ID, j.Worker, n)
			last = time.Now()
		}
	})
	var outputs []Output
	if er == nil {
		files, fe := outputFiles(dir)
		er = fe
		if er == nil && len(files) == 0 {
			er = fmt.Errorf("el motor no generó archivos; el archivo puede exceder el límite")
		}
		if er == nil {
			var ok bool
			ok, er = s.Store.Uploading(ctx, j)
			if !ok && er == nil {
				er = context.Canceled
			}
		}
		if er == nil {
			for _, path := range files {
				info, ie := os.Stat(path)
				if ie != nil {
					er = ie
					break
				}
				if shouldTranscodeVideo(ctx, s.Config.FFmpeg, path, j.Options) {
					s.Store.Stage(ctx, j, "waiting", "Esperando turno de GPU/RAM", 0)
					select {
					case s.ramGate <- struct{}{}:
					case <-ctx.Done():
						er = ctx.Err()
					}
					if er != nil {
						break
					}
					var parts []Output
					parts, er = s.sendSplit(ctx, j, path, true)
					<-s.ramGate
					outputs = append(outputs, parts...)
				} else if info.Size() > s.Config.MaxBytes && j.Options.Split {
					s.Store.Stage(ctx, j, "waiting", "Esperando turno de GPU/RAM", 0)
					select {
					case s.ramGate <- struct{}{}:
					case <-ctx.Done():
						er = ctx.Err()
					}
					if er != nil {
						break
					}
					var parts []Output
					parts, er = s.sendSplit(ctx, j, path, false)
					<-s.ramGate
					outputs = append(outputs, parts...)
				} else if info.Size() > s.Config.MaxBytes {
					if s.Config.Admins[j.UserID] {
						er = fmt.Errorf("el archivo supera el límite del bot (%d MB); reduce calidad o usa Mensajes guardados del propietario", s.Config.MaxBytes/1000000)
					} else {
						er = fmt.Errorf("el archivo (%.0f MB) supera el límite de Telegram (%d MB); reduce calidad o activa Premium", float64(info.Size())/1000000, s.Config.MaxBytes/1000000)
					}
				} else if s.Bot != nil {
					kind := "document"
					if !j.Options.AsDocument {
						if j.Options.Mode == "audio" && (strings.HasSuffix(path, ".mp3") || strings.HasSuffix(path, ".m4a")) {
							kind = "audio"
						} else if strings.HasSuffix(path, ".mp4") && !j.Options.TranscodeRAM {
							kind = "video"
						}
					}
					var id string
					uploadProgress := func(done, total int64) {
						pct := float64(0)
						if total > 0 {
							pct = float64(done) * 100 / float64(total)
						}
						detail := fmt.Sprintf("Enviando a Telegram · %.1f / %.1f MB (%.0f%%)", float64(done)/1e6, float64(total)/1e6, pct)
						if done == total {
							detail = "Archivo transferido; esperando confirmación de Telegram"
						}
						s.Store.Stage(ctx, j, "uploading", detail, pct)
					}
					id, er = s.uploadSlot(ctx, func() (string, error) {
						return s.Bot.UploadWithProgress(ctx, j.ChatID, path, kind, uploadProgress)
					})
					if er != nil && kind != "document" && ctx.Err() == nil {
						kind = "document"
						id, er = s.uploadSlot(ctx, func() (string, error) {
							return s.Bot.UploadWithProgress(ctx, j.ChatID, path, "document", uploadProgress)
						})
					}
					if er == nil {
						outputs = append(outputs, Output{Name: filepath.Base(path), Size: info.Size(), FileID: id, Kind: kind})
					}
				} else {
					er = fmt.Errorf("BOT_TOKEN pendiente: archivo descargado localmente, entrega Telegram no disponible")
				}
				if er != nil {
					break
				}
			}
		}
	}
	cancel()
	<-watchDone
	finishCtx, done := context.WithTimeout(context.Background(), 10*time.Second)
	defer done()
	state, msg := "done", ""
	if er != nil {
		state = "failed"
		msg = er.Error()
		if errors.Is(er, context.DeadlineExceeded) || parent.Err() != nil {
			msg = "tarea interrumpida o tiempo máximo alcanzado; puedes reintentar"
		}
		reasonMu.Lock()
		if reason != "" {
			msg = reason
		}
		reasonMu.Unlock()
	}
	if fe := s.Store.Finish(finishCtx, j, state, msg, outputs); fe != nil {
		slog.Error("no se pudo persistir resultado de tarea", "id", j.ID)
	}
	notify := s.Bot != nil && parent.Err() == nil
	if notify {
		var finalState string
		if s.Store.Pool.QueryRow(finishCtx, "SELECT state FROM jobs WHERE id=$1", j.ID).Scan(&finalState) == nil && finalState == "cancelled" {
			notify = false
		}
	}
	if notify {
		if er == nil {
			text := "✅ Descarga completada. Consulta tus archivos en la Mini App."
			for _, out := range outputs {
				if out.SavedMessageID > 0 {
					text = "✅ Archivo completo enviado a tus Mensajes guardados mediante tu cuenta Premium. Consulta el historial en la Mini App."
					break
				}
			}
			s.Bot.Message(finishCtx, j.ChatID, text, s.menu())
		} else {
			s.Bot.Message(finishCtx, j.ChatID, "La tarea terminó: "+msg, s.menu())
		}
	}
	// Partial files are only removed inside this job's validated workspace.
	if er != nil && jobIDPattern.MatchString(j.ID) {
		os.RemoveAll(dir)
	}
}
func isVideo(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".mp4", ".mkv", ".webm", ".mov", ".avi", ".m4v":
		return true
	}
	return false
}
func outputFiles(dir string) ([]string, error) {
	if b, err := os.ReadFile(filepath.Join(dir, ".finalfiles.json")); err == nil {
		var paths []string
		if err = json.Unmarshal(b, &paths); err != nil {
			return nil, err
		}
		base, _ := filepath.Abs(dir)
		result := []string{}
		for _, p := range paths {
			absolute, err := filepath.Abs(p)
			if err != nil {
				return nil, err
			}
			relative, err := filepath.Rel(base, absolute)
			if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
				return nil, fmt.Errorf("salida fuera del directorio de tarea")
			}
			info, err := os.Lstat(absolute)
			if err != nil {
				return nil, err
			}
			if !info.Mode().IsRegular() || info.Size() == 0 {
				return nil, fmt.Errorf("el motor produjo un archivo final vacío o inválido")
			}
			result = append(result, absolute)
		}
		err = filepath.WalkDir(base, func(path string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if d.IsDir() {
				return nil
			}
			ext := strings.ToLower(filepath.Ext(path))
			if ext != ".srt" && ext != ".vtt" && ext != ".ass" {
				return nil
			}
			info, statErr := os.Lstat(path)
			if statErr != nil {
				return statErr
			}
			if info.Mode().IsRegular() && info.Size() > 0 {
				result = append(result, path)
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
		if len(result) > 200 {
			return nil, fmt.Errorf("demasiados archivos")
		}
		return result, nil
	}
	paths := []string{}
	e := filepath.WalkDir(dir, func(path string, d fs.DirEntry, er error) error {
		if er != nil {
			return er
		}
		if d.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("enlace simbólico rechazado")
		}
		if d.IsDir() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		switch ext {
		case ".part", ".ytdl", ".temp", ".json":
			return nil
		}
		if strings.Contains(filepath.Base(path), ".part-") {
			return nil
		}
		paths = append(paths, path)
		if len(paths) > 200 {
			return fmt.Errorf("demasiados archivos; reduce el lote")
		}
		return nil
	})
	sort.Strings(paths)
	return paths, e
}
func (s *Server) maintenance(ctx context.Context) {
	// One app instance owns the queue lock. Jobs with no heartbeat after a crash
	// are failed, never silently duplicated during an uncertain Telegram upload.
	s.Store.Pool.Exec(ctx, "UPDATE jobs SET state='failed',error='El servicio se interrumpió. Revisa archivos recibidos antes de reintentar.',updated_at=now() WHERE state IN ('downloading','uploading') AND updated_at<now()-interval '2 minutes'")
	rows, er := s.Store.Pool.Query(ctx, "SELECT id FROM jobs WHERE state IN ('done','failed','cancelled') AND updated_at<$1", time.Now().Add(-s.Config.Retention))
	if er != nil {
		return
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil && jobIDPattern.MatchString(id) {
			ids = append(ids, id)
		}
	}
	rows.Close()
	for _, id := range ids {
		os.RemoveAll(filepath.Join(s.Config.DataDir, "jobs", id))
	}
}
