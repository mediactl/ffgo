//go:build !ios && !android && (amd64 || arm64)

package ffgo

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/obinnaokechukwu/ffgo/internal/bindings"
)

// ffgo's Go struct offsets were written for FFmpeg 4-7. On a newer release
// without the shim built against its headers, Init must refuse rather than
// read structs at offsets that moved (most of AVCodecContext and
// AVFormatContext did, in 9.0).
func TestInitRefusesANewerFFmpegWithoutAMatchingShim(t *testing.T) {
	if os.Getenv("FFGO_CHILD_NO_SHIM") == "" {
		if !requireFFmpeg(t) {
			t.Skip("FFmpeg not available")
		}
		if set, _ := bindings.LoadedVersionSet(); set.FFmpeg <= 7 {
			t.Skipf("FFmpeg %d: Go offsets apply without a shim", set.FFmpeg)
		}
		dir := t.TempDir()
		// FFGO_SHIM_DIR is searched first; a file that is not a library
		// leaves the process with no shim at all.
		if err := os.WriteFile(filepath.Join(dir, "libffshim.so"), []byte("not a library"), 0o644); err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command(os.Args[0], "-test.run=^TestInitRefusesANewerFFmpegWithoutAMatchingShim$", "-test.count=1", "-test.v")
		cmd.Env = append(os.Environ(), "FFGO_CHILD_NO_SHIM=1", "FFGO_SHIM_DIR="+dir)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("child: %v\n%s", err, out)
		}
		if !strings.Contains(string(out), "--- PASS") {
			t.Fatalf("child did not pass:\n%s", out)
		}
		return
	}
	err := Init()
	if err == nil {
		t.Fatal("Init succeeded on FFmpeg newer than 7 with no shim")
	}
	if !strings.Contains(err.Error(), "needs the ffshim built against its headers") {
		t.Fatalf("Init error does not say what is missing: %v", err)
	}
	t.Log(err)

	// The constructors are where most callers start (the README never calls
	// Init); they must refuse the same way, not read and write structs at
	// FFmpeg 6/7 offsets.
	for name, open := range map[string]func() error{
		"NewDecoder":  func() error { _, err := NewDecoder("/nonexistent.mkv"); return err },
		"ProbeFormat": func() error { _, err := ProbeFormat("/nonexistent.mkv"); return err },
		"NewEncoderWithOptions": func() error {
			_, err := NewEncoderWithOptions(filepath.Join(os.TempDir(), "ffgo-noshim.mkv"), &EncoderOptions{
				Video: &VideoEncoderConfig{Width: 64, Height: 64, FrameRate: NewRational(25, 1)},
			})
			return err
		},
	} {
		if err := open(); err == nil || !strings.Contains(err.Error(), "needs the ffshim built against its headers") {
			t.Errorf("%s on FFmpeg newer than 7 with no shim: got %v, want the shim refusal", name, err)
		}
	}
}
