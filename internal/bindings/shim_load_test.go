//go:build !ios && !android && (amd64 || arm64)

package bindings

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// Loading FFmpeg must not open a shim on the library path: a shim links the
// FFmpeg release it was built against, and opening one from another release
// maps that release's libraries beside the loaded ones. With FFmpeg 4.4
// pinned and the FFmpeg 9 prebuilt on LD_LIBRARY_PATH, libavcodec.so.63 must
// not appear in the process.
func TestLoadDoesNotOpenAShimFromTheLibraryPath(t *testing.T) {
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		t.Skip("uses the linux-amd64 prebuilt and /proc/self/maps")
	}
	if os.Getenv("FFGO_CHILD_SHIM_PATH") == "" {
		prebuilt, err := filepath.Abs(filepath.Join("..", "..", "shim", "prebuilt", "linux-amd64"))
		if err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command(os.Args[0], "-test.run=^TestLoadDoesNotOpenAShimFromTheLibraryPath$", "-test.count=1", "-test.v")
		cmd.Env = append(os.Environ(), "FFGO_CHILD_SHIM_PATH=1", "FFGO_FFMPEG_MAJOR=4", "LD_LIBRARY_PATH="+prebuilt)
		out, err := cmd.CombinedOutput()
		if strings.Contains(string(out), "--- SKIP") {
			t.Skipf("child skipped:\n%s", out)
		}
		if err != nil {
			t.Fatalf("child: %v\n%s", err, out)
		}
		return
	}
	if err := Load(); err != nil {
		t.Skipf("FFmpeg 4.4 not on this host: %v", err)
	}
	maps, err := os.ReadFile("/proc/self/maps")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(maps), "libavcodec.so.63") {
		t.Fatal("FFmpeg 4.4 is loaded, and libavcodec.so.63 is mapped too: a shim from the library path pulled FFmpeg 9 in")
	}
}
