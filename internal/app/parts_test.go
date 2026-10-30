package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"testing"
)

func TestPartsExactAndBounded(t *testing.T) {
	for _, size := range []int{1, 7, 8, 9, 24, 25, 10001} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			source := bytes.Repeat([]byte{0, 3, 255, 8, 7}, size)
			var reconstructed bytes.Buffer
			out, m, e := SendParts(context.Background(), bytes.NewReader(source), "video.mp4", 8, func(n string, r io.Reader) (string, error) {
				b, e := io.ReadAll(r)
				if len(b) > 8 {
					t.Fatal("parte excedida")
				}
				reconstructed.Write(b)
				return n, e
			})
			if e != nil {
				t.Fatal(e)
			}
			if !bytes.Equal(source, reconstructed.Bytes()) || len(out) != len(m.Parts) {
				t.Fatal("contenido alterado")
			}
			h := sha256.Sum256(source)
			if m.SHA256 != hex.EncodeToString(h[:]) {
				t.Fatal("hash incorrecto")
			}
		})
	}
}
func TestPartsStopsOnFailedUpload(t *testing.T) {
	calls := 0
	_, _, e := SendParts(context.Background(), bytes.NewReader(make([]byte, 100)), "file.zip", 8, func(_ string, _ io.Reader) (string, error) { calls++; return "", fmt.Errorf("fallo") })
	if e == nil || calls != 1 {
		t.Fatal("continuó tras fallo")
	}
}
func TestOptionsRejectUnsafeArguments(t *testing.T) {
	for _, o := range []Options{{Mode: "--exec"}, {Quality: 9999}, {AudioFormat: "mp3;rm"}, {Engine: "jdownloader;echo"}, {Start: 30, End: 2}, {Mode: "audio", TranscodeRAM: true}} {
		if o.Validate() == nil {
			t.Fatalf("aceptó %+v", o)
		}
	}
}
