package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestOnlyFinalMediaAndSubtitlesAreDelivered(t *testing.T) {
	dir := t.TempDir()
	final := filepath.Join(dir, "final.mp4")
	os.WriteFile(final, []byte("final"), 0600)
	os.WriteFile(filepath.Join(dir, "intermediate.f243.webm"), []byte("intermediate"), 0600)
	os.WriteFile(filepath.Join(dir, "empty.mp4"), nil, 0600)
	os.WriteFile(filepath.Join(dir, "sub.es.srt"), []byte("subtitle"), 0600)
	b, _ := json.Marshal([]string{final})
	os.WriteFile(filepath.Join(dir, ".finalfiles.json"), b, 0600)
	paths, e := outputFiles(dir)
	if e != nil || len(paths) != 2 || paths[0] != final {
		t.Fatal(paths, e)
	}
}
