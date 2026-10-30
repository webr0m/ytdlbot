package app

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"webrom/ytdlbot/internal/safeurl"
)

type tail struct {
	sync.Mutex
	b []byte
}

func (t *tail) Write(p []byte) (int, error) {
	t.Lock()
	defer t.Unlock()
	t.b = append(t.b, p...)
	if len(t.b) > 8192 {
		t.b = append([]byte{}, t.b[len(t.b)-8192:]...)
	}
	return len(p), nil
}
func (t *tail) String() string { t.Lock(); defer t.Unlock(); return string(t.b) }

type Engine struct {
	Config   Config
	ProxyURL string
	HTTP     *http.Client
}

func (e *Engine) base(j Job) []string {
	ffmpeg := e.Config.FFmpeg
	if resolved, err := exec.LookPath(ffmpeg); err == nil {
		ffmpeg = resolved
	}
	a := []string{"--ignore-config", "--no-plugin-dirs", "--no-colors", "--no-warnings", "--js-runtimes", "node", "--proxy", e.ProxyURL, "--socket-timeout", "30", "--retries", "3", "--fragment-retries", "3", "--concurrent-fragments", "2", "--hls-prefer-native", "--ffmpeg-location", ffmpeg, "--no-cache-dir"}
	if !j.Options.Split {
		a = append(a, "--max-filesize", strconv.FormatInt(e.Config.MaxBytes, 10))
	}
	if j.Options.UseCookies && e.Config.Cookies != "" {
		a = append(a, "--cookies", e.Config.Cookies)
	}
	if j.Options.Playlist {
		a = append(a, "--yes-playlist", "--playlist-end", strconv.Itoa(e.Config.PlaylistLimit))
	} else {
		a = append(a, "--no-playlist")
	}
	return a
}
func (e *Engine) Inspect(ctx context.Context, raw string) (MediaInfo, error) {
	if er := safeurl.Validate(ctx, raw); er != nil {
		return MediaInfo{}, er
	}
	j := Job{URL: raw}
	a := e.base(j)
	a = append(a, "--dump-single-json", "--skip-download", "--", raw)
	cmd := exec.CommandContext(ctx, e.Config.YTDLP, a...)
	prepareProcess(cmd)
	out, er := cmd.StdoutPipe()
	if er != nil {
		return MediaInfo{}, er
	}
	var stderr tail
	cmd.Stderr = &stderr
	if er = cmd.Start(); er != nil {
		return MediaInfo{}, fmt.Errorf("yt-dlp no está instalado")
	}
	var info MediaInfo
	// Limit metadata before parsing: playlists and formats cannot exhaust memory.
	b, er := io.ReadAll(io.LimitReader(out, 4<<20))
	if er == nil {
		er = decodeInfo(b, &info)
	}
	if er != nil {
		cmd.Cancel()
	}
	wait := cmd.Wait()
	if er != nil || wait != nil {
		return info, fmt.Errorf("no se pudo analizar el enlace; puede necesitar acceso, cookies o una actualización del motor")
	}
	return info, nil
}
func (e *Engine) Download(ctx context.Context, j Job, dir string, progress func(float64)) error {
	if er := safeurl.Validate(ctx, j.URL); er != nil {
		return er
	}
	os.MkdirAll(dir, 0700)
	if paths, err := outputFiles(dir); err == nil && len(paths) > 0 {
		if _, err = os.Stat(filepath.Join(dir, ".finalfiles.json")); err == nil {
			return nil
		}
	}
	if strings.HasPrefix(j.URL, "https://t.me/") && e.Config.MTProto != "" {
		if !e.Config.Admins[j.UserID] {
			return fmt.Errorf("la sesión MTProto del propietario está reservada a administradores")
		}
		cmd := exec.CommandContext(ctx, e.Config.MTProto, "-url", j.URL, "-out", dir)
		prepareProcess(cmd)
		var result tail
		cmd.Stderr = &result
		if er := cmd.Run(); er != nil {
			return fmt.Errorf("MTProto no pudo descargar el mensaje; comprueba la sesión local y el acceso al canal")
		}
		return nil
	}
	engine := j.Options.Engine
	if engine == "auto" {
		engine = "ytdlp"
	}
	if engine == "direct" {
		return e.direct(ctx, j, dir, progress)
	}
	var exe string
	var a []string
	if engine == "gallery" {
		if !e.Config.Gallery {
			return fmt.Errorf("galerías deshabilitadas")
		}
		exe = e.Config.GalleryDL
		a = []string{"--ignore-config", "--proxy", e.ProxyURL, "--range", "1-" + strconv.Itoa(e.Config.PlaylistLimit), "--destination", dir, "-o", "extractor.retries=3", "-o", "extractor.timeout=30"}
		if j.Options.UseCookies && e.Config.Cookies != "" {
			a = append(a, "--cookies", e.Config.Cookies)
		}
		a = append(a, "--", j.URL)
	} else {
		exe = e.Config.YTDLP
		a = e.base(j)
		a = append(a, "--no-simulate", "--print", "after_move:FINAL:%(filepath)j")
		a = append(a, "--newline", "--progress", "--progress-template", "download:PROGRESS:%(progress._percent_str)s", "--restrict-filenames", "--windows-filenames", "-o", filepath.Join(dir, "%(playlist_index)s_%(title).100B_%(id)s.%(ext)s"))
		if j.Options.Mode == "audio" {
			a = append(a, "-f", "bestaudio/best", "--extract-audio", "--audio-format", j.Options.AudioFormat, "--audio-quality", "0")
		} else {
			// Prefer a stream pair Telegram can use directly. This avoids a
			// needless re-encode of 1080p and lower YouTube downloads; if no
			// H.264/AAC pair exists (for example 4K), yt-dlp falls back to the
			// best available streams and the worker decides whether to convert.
			format := videoFormat(j.Options.Quality)
			a = append(a, "-f", format, "--merge-output-format", "mp4")
		}
		if j.Options.Subtitles {
			a = append(a, "--write-subs", "--write-auto-subs", "--sub-langs", "es.*,en.*", "--convert-subs", "srt")
		}
		if j.Options.Start > 0 || j.Options.End > 0 {
			end := "inf"
			if j.Options.End > 0 {
				end = strconv.Itoa(j.Options.End)
			}
			a = append(a, "--download-sections", fmt.Sprintf("*%d-%s", j.Options.Start, end))
		}
		a = append(a, "--", j.URL)
	}
	cmd := exec.CommandContext(ctx, exe, a...)
	prepareProcess(cmd)
	cmd.WaitDelay = 10 * time.Second
	pipe, er := cmd.StdoutPipe()
	if er != nil {
		return er
	}
	var stderr tail
	cmd.Stderr = &stderr
	if er = cmd.Start(); er != nil {
		return fmt.Errorf("motor %s no disponible; ejecuta setup", engine)
	}
	sc := bufio.NewScanner(pipe)
	finals := []string{}
	sc.Buffer(make([]byte, 4096), 256*1024)
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "FINAL:") {
			var path string
			if json.Unmarshal([]byte(strings.TrimPrefix(line, "FINAL:")), &path) == nil {
				finals = append(finals, path)
			}
		}
		if strings.HasPrefix(line, "PROGRESS:") {
			p := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(line, "PROGRESS:"), "%"))
			if n, err := strconv.ParseFloat(p, 64); err == nil {
				progress(n)
			}
		}
	}
	if sc.Err() != nil {
		cmd.Cancel()
	}
	er = cmd.Wait()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if er != nil {
		return friendlyError(stderr.String())
	}
	if sc.Err() != nil {
		return sc.Err()
	}
	if engine != "gallery" {
		b, _ := json.Marshal(finals)
		return os.WriteFile(filepath.Join(dir, ".finalfiles.json"), b, 0600)
	}
	return nil
}
func videoFormat(quality int) string {
	return fmt.Sprintf("bv*[height<=%d][vcodec^=avc]+ba[acodec^=mp4a]/b[height<=%d]/bv*[height<=%d]+ba/b", quality, quality, quality)
}
func friendlyError(detail string) error {
	d := strings.ToLower(detail)
	switch {
	case strings.Contains(d, "sign in") || strings.Contains(d, "cookies") || strings.Contains(d, "login"):
		return fmt.Errorf("el sitio requiere una sesión autorizada; un administrador puede configurar cookies")
	case strings.Contains(d, "drm"):
		return fmt.Errorf("contenido protegido con DRM; el motor no puede descargarlo")
	case strings.Contains(d, "unsupported url"):
		return fmt.Errorf("sitio no reconocido; prueba Archivo directo o Galería si corresponde")
	case strings.Contains(d, "429"):
		return fmt.Errorf("el sitio limitó las solicitudes; espera antes de reintentar")
	case strings.Contains(d, "max-filesize") || strings.Contains(d, "larger than max"):
		return fmt.Errorf("el archivo excede el máximo de entrega; reduce calidad o usa el transporte Premium del propietario")
	default:
		return fmt.Errorf("el motor no pudo completar la descarga; revisa acceso al enlace o actualiza los motores")
	}
}
func (e *Engine) direct(ctx context.Context, j Job, dir string, progress func(float64)) error {
	req, er := http.NewRequestWithContext(ctx, "GET", j.URL, nil)
	if er != nil {
		return er
	}
	req.Header.Set("User-Agent", "MultiDownloaderPro/0.1")
	resp, er := e.HTTP.Do(req)
	if er != nil {
		return fmt.Errorf("no se pudo descargar el archivo público")
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("el servidor respondió HTTP %d", resp.StatusCode)
	}
	if !j.Options.Split && resp.ContentLength > e.Config.MaxBytes {
		return fmt.Errorf("archivo demasiado grande; activa Procesar archivos grandes con Premium dentro del límite de entrega")
	}
	name := "archivo.bin"
	if ext := filepath.Ext(resp.Request.URL.Path); len(ext) > 1 && len(ext) < 12 && !strings.ContainsAny(ext, "/\\:") {
		name = "archivo" + ext
	}
	f, er := os.OpenFile(filepath.Join(dir, name), os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0600)
	if er != nil {
		return er
	}
	defer f.Close()
	var n int64
	buf := make([]byte, 256*1024)
	last := time.Now()
	for {
		got, re := resp.Body.Read(buf)
		if got > 0 {
			n += int64(got)
			if !j.Options.Split && n > e.Config.MaxBytes {
				return fmt.Errorf("archivo excede el máximo")
			}
			if _, er = f.Write(buf[:got]); er != nil {
				return er
			}
			if time.Since(last) > time.Second && resp.ContentLength > 0 {
				progress(float64(n) * 100 / float64(resp.ContentLength))
				last = time.Now()
			}
		}
		if re == io.EOF {
			break
		}
		if re != nil {
			return re
		}
	}
	return nil
}
