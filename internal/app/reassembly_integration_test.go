package app

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestMultiPartReassemblyAcrossGoAndNode(t *testing.T) {
	node, e := exec.LookPath("node")
	if e != nil {
		t.Skip("Node no disponible")
	}
	dir := t.TempDir()
	payload := bytes.Repeat([]byte{1, 2, 3, 4, 0, 255, 10, 20}, 10*1024*1024)
	out, m, e := SendParts(context.Background(), bytes.NewReader(payload), "original.bin", 40*1000*1000, func(name string, r io.Reader) (string, error) {
		f, e := os.Create(filepath.Join(dir, name))
		if e != nil {
			return "", e
		}
		_, e = io.Copy(f, r)
		f.Close()
		return "verified", e
	})
	if e != nil {
		t.Fatal(e)
	}
	if len(out) != 3 || out[0].Size != 32*1024*1024 {
		t.Fatal("primer bloque o cantidad incorrectos", out)
	}
	b, _ := json.Marshal(m)
	manifest := filepath.Join(dir, "original.manifest.json")
	os.WriteFile(manifest, b, 0600)
	cmd := exec.Command(node, filepath.Join("..", "..", "scripts", "join-parts.mjs"), manifest)
	if b, e = cmd.CombinedOutput(); e != nil {
		t.Fatal(string(b), e)
	}
	reconstructed, e := os.ReadFile(filepath.Join(dir, "original.bin"))
	if e != nil || !bytes.Equal(payload, reconstructed) {
		t.Fatal("contenido reconstruido diferente", e)
	}
}
