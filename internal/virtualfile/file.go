// Package virtualfile exposes binary parts as one verified, sequential file.
// It opens one part at a time and never allocates the size of the entire file.
package virtualfile

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
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
type File struct {
	Manifest Manifest
	dir      string
}

func safeName(n string) bool {
	return n != "" && n != "." && n != ".." && filepath.Base(n) == n && !strings.ContainsAny(n, "/\\:\x00\r\n")
}
func validHash(h string) bool { b, e := hex.DecodeString(h); return e == nil && len(b) == 32 }

// Open validates all sizes and hashes before the first byte can be uploaded.
func Open(ctx context.Context, path string) (*File, error) {
	f, e := os.Open(path)
	if e != nil {
		return nil, e
	}
	defer f.Close()
	var m Manifest
	if e = json.NewDecoder(io.LimitReader(f, 16<<20)).Decode(&m); e != nil {
		return nil, e
	}
	if m.Version != 1 || !safeName(m.Original) || m.Size <= 0 || !validHash(m.SHA256) || len(m.Parts) == 0 || len(m.Parts) > 100000 {
		return nil, fmt.Errorf("manifiesto inválido")
	}
	v := &File{Manifest: m, dir: filepath.Dir(path)}
	seen := map[string]bool{}
	var size int64
	total := sha256.New()
	buf := make([]byte, 1<<20)
	for _, p := range m.Parts {
		if !safeName(p.Name) || seen[p.Name] || p.Size <= 0 || p.Size > m.Size-size || !validHash(p.SHA256) {
			return nil, fmt.Errorf("parte inválida")
		}
		seen[p.Name] = true
		path := filepath.Join(v.dir, p.Name)
		info, e := os.Lstat(path)
		if e != nil || !info.Mode().IsRegular() || info.Size() != p.Size {
			return nil, fmt.Errorf("parte ausente o tamaño incorrecto: %s", p.Name)
		}
		r, e := os.Open(path)
		if e != nil {
			return nil, e
		}
		h := sha256.New()
		n, e := io.CopyBuffer(io.MultiWriter(h, total), &contextReader{ctx: ctx, r: r}, buf)
		r.Close()
		if e != nil {
			return nil, e
		}
		if n != p.Size || !strings.EqualFold(hex.EncodeToString(h.Sum(nil)), p.SHA256) {
			return nil, fmt.Errorf("SHA-256 incorrecto: %s", p.Name)
		}
		size += n
	}
	if size != m.Size || !strings.EqualFold(hex.EncodeToString(total.Sum(nil)), m.SHA256) {
		return nil, fmt.Errorf("SHA-256 o tamaño total incorrecto")
	}
	return v, nil
}

type contextReader struct {
	ctx context.Context
	r   io.Reader
}

func (r *contextReader) Read(p []byte) (int, error) {
	if e := r.ctx.Err(); e != nil {
		return 0, e
	}
	return r.r.Read(p)
}

type reader struct {
	ctx     context.Context
	v       *File
	index   int
	current *os.File
	left    int64
	closed  bool
}

func (v *File) Reader(ctx context.Context) io.ReadCloser { return &reader{ctx: ctx, v: v} }
func (r *reader) Close() error {
	r.closed = true
	if r.current != nil {
		return r.current.Close()
	}
	return nil
}
func (r *reader) Read(p []byte) (int, error) {
	if r.closed {
		return 0, os.ErrClosed
	}
	if len(p) == 0 {
		return 0, nil
	}
	if e := r.ctx.Err(); e != nil {
		return 0, e
	}
	for {
		if r.current == nil {
			if r.index == len(r.v.Manifest.Parts) {
				return 0, io.EOF
			}
			part := r.v.Manifest.Parts[r.index]
			f, e := os.Open(filepath.Join(r.v.dir, part.Name))
			if e != nil {
				return 0, e
			}
			r.current = f
			r.left = part.Size
		}
		if r.left == 0 {
			r.current.Close()
			r.current = nil
			r.index++
			continue
		}
		if int64(len(p)) > r.left {
			p = p[:r.left]
		}
		n, e := r.current.Read(p)
		r.left -= int64(n)
		if e == io.EOF && r.left > 0 {
			e = io.ErrUnexpectedEOF
		}
		return n, e
	}
}

// Stage stores bounded blocks to disk so encoder success and the final size are
// known before sending a single Telegram message. No joined copy is created.
func Stage(ctx context.Context, source io.Reader, dir, name string, partBytes, maxBytes int64) (string, error) {
	if !safeName(name) || partBytes < 1 || maxBytes < 1 {
		return "", fmt.Errorf("límites inválidos")
	}
	m := Manifest{Version: 1, Original: name, Note: "Bloques internos; entrega como un único archivo unido virtualmente."}
	total := sha256.New()
	buf := make([]byte, 1<<20)
	for i := 1; ; i++ {
		part := Part{Name: fmt.Sprintf("%s.part%05d", name, i)}
		p := filepath.Join(dir, part.Name)
		f, e := os.OpenFile(p, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if e != nil {
			return "", e
		}
		h := sha256.New()
		limit := min(partBytes, maxBytes-m.Size+1)
		n, e := io.CopyBuffer(io.MultiWriter(f, h, total), io.LimitReader(&contextReader{ctx: ctx, r: source}, limit), buf)
		closeErr := f.Close()
		if e != nil {
			return "", e
		}
		if closeErr != nil {
			return "", closeErr
		}
		if n == 0 {
			os.Remove(p)
			break
		}
		m.Size += n
		if m.Size > maxBytes {
			return "", fmt.Errorf("archivo unido supera el máximo de %.0f MB del transporte; reduce la calidad o recorta el vídeo", float64(maxBytes)/1e6)
		}
		part.Size = n
		part.SHA256 = hex.EncodeToString(h.Sum(nil))
		m.Parts = append(m.Parts, part)
		if n < limit {
			break
		}
	}
	if m.Size == 0 {
		return "", fmt.Errorf("archivo vacío")
	}
	m.SHA256 = hex.EncodeToString(total.Sum(nil))
	b, e := json.MarshalIndent(m, "", "  ")
	if e != nil {
		return "", e
	}
	path := filepath.Join(dir, name+".manifest.json")
	return path, os.WriteFile(path, b, 0600)
}
