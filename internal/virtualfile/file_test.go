package virtualfile

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestVirtualJoinExactAndNoJoinedCopy(t *testing.T) {
	for _, n := range []int{1, 7, 8, 9, 128, 129} {
		t.Run(string(rune(n+65)), func(t *testing.T) {
			dir := t.TempDir()
			payload := bytes.Repeat([]byte{0, 255, 33, 6}, n)
			manifest, e := Stage(context.Background(), bytes.NewReader(payload), dir, "video.mp4", 32, 10000)
			if e != nil {
				t.Fatal(e)
			}
			v, e := Open(context.Background(), manifest)
			if e != nil {
				t.Fatal(e)
			}
			r := v.Reader(context.Background())
			defer r.Close()
			got, e := io.ReadAll(r)
			if e != nil {
				t.Fatal(e)
			}
			if !bytes.Equal(got, payload) || v.Manifest.Size != int64(len(payload)) {
				t.Fatal("joined contents differ")
			}
			if _, e = os.Stat(filepath.Join(dir, "video.mp4")); !os.IsNotExist(e) {
				t.Fatal("created a joined disk copy")
			}
		})
	}
}
func TestRefuseCorruptMissingAndTraversalParts(t *testing.T) {
	for _, mutation := range []string{"corrupt", "missing", "traversal", "duplicate", "total"} {
		t.Run(mutation, func(t *testing.T) {
			dir := t.TempDir()
			path, e := Stage(context.Background(), strings.NewReader("abcdefgh"), dir, "output.mp4", 4, 100)
			if e != nil {
				t.Fatal(e)
			}
			b, _ := os.ReadFile(path)
			var m Manifest
			json.Unmarshal(b, &m)
			switch mutation {
			case "corrupt":
				os.WriteFile(filepath.Join(dir, m.Parts[0].Name), []byte("XXXX"), 0600)
			case "missing":
				os.Remove(filepath.Join(dir, m.Parts[0].Name))
			case "traversal":
				m.Parts[0].Name = "../outside"
			case "duplicate":
				m.Parts[1] = m.Parts[0]
			case "total":
				m.Size++
			}
			b, _ = json.Marshal(m)
			os.WriteFile(path, b, 0600)
			if _, e = Open(context.Background(), path); e == nil {
				t.Fatal("accepted invalid parts")
			}
		})
	}
}
func TestSizeLimitAndCancellation(t *testing.T) {
	for _, n := range []int{7, 8, 9} {
		_, e := Stage(context.Background(), strings.NewReader(strings.Repeat("x", n)), t.TempDir(), "out.bin", 4, 8)
		if (e != nil) != (n > 8) {
			t.Fatalf("size %d: %v", n, e)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e := Stage(ctx, strings.NewReader("abc"), t.TempDir(), "out.bin", 2, 10); e != context.Canceled {
		t.Fatal(e)
	}
}
