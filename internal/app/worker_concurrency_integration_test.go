package app

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"webrom/ytdlbot/internal/safeurl"
	"webrom/ytdlbot/internal/telegram"
)

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestTwoJobsUploadConcurrentlyThroughOneBot(t *testing.T) {
	store := testStore(t)
	c := store.Config
	c.DataDir = t.TempDir()
	c.Workers = 2
	c.MaxActivePerUser = 2
	c.UploadConcurrency = 2
	c.MaxBytes = 1000000
	c.Timeout = time.Minute
	c.Retention = 24 * time.Hour
	store.Config = c
	var active, peak, seen atomic.Int32
	barrier := make(chan struct{})
	var once sync.Once
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "sendDocument") {
			if e := r.ParseMultipartForm(1 << 20); e != nil {
				t.Error(e)
				return
			}
			defer r.MultipartForm.RemoveAll()
			a := active.Add(1)
			defer active.Add(-1)
			for {
				p := peak.Load()
				if a <= p || peak.CompareAndSwap(p, a) {
					break
				}
			}
			if seen.Add(1) == 2 {
				once.Do(func() { close(barrier) })
			}
			select {
			case <-barrier:
			case <-r.Context().Done():
				return
			}
			io.WriteString(w, `{"ok":true,"result":{"document":{"file_id":"test"}}}`)
		} else {
			io.WriteString(w, `{"ok":true,"result":true}`)
		}
	}))
	defer mock.Close()
	engine := &Engine{Config: c, HTTP: &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, ContentLength: 100, Body: io.NopCloser(bytes.NewReader(make([]byte, 100))), Header: make(http.Header), Request: r}, nil
	})}}
	server := NewServer(c, store, engine)
	server.Bot = telegram.New(mock.URL, "test")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if e := safeurl.Validate(ctx, "https://1.1.1.1/file.bin"); e != nil {
		t.Fatal(e)
	}
	for i := 0; i < 2; i++ {
		if _, e := store.Create(ctx, 999, 999, "https://1.1.1.1/file.bin", Options{Mode: "file"}); e != nil {
			t.Fatal(e)
		}
	}
	var wg sync.WaitGroup
	server.Workers(ctx, &wg)
	ticker := time.NewTicker(30 * time.Millisecond)
	defer ticker.Stop()
	complete := false
	for !complete {
		select {
		case <-ctx.Done():
			t.Error("la concurrencia no permitió completar ambas tareas")
			complete = true
		case <-ticker.C:
			jobs, e := store.List(ctx, 999)
			if e != nil {
				continue
			}
			if len(jobs) == 2 && jobs[0].State == "done" && jobs[1].State == "done" {
				complete = true
			}
		}
	}
	cancel()
	wg.Wait()
	if peak.Load() != 2 {
		t.Fatalf("subidas simultáneas máximas: %d", peak.Load())
	}
}
