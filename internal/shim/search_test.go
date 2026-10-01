//go:build !ios && !android && (amd64 || arm64)

package shim

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// A shim built for another FFmpeg release must not end the search: a
// matching one later in the search path is the one to load. (On Debian 12
// the module's FFmpeg 9 prebuilt was found first and rejected, no shim was
// loaded, and FFmpeg 5.1's structs were read at FFmpeg 6/7 offsets.)
func TestShimSearchSkipsACandidateForAnotherRelease(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("linux search paths")
	}
	wrong, right := t.TempDir(), t.TempDir()
	for _, d := range []string{wrong, right} {
		if err := os.WriteFile(filepath.Join(d, "libffshim.so"), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("FFGO_SHIM_DIR", "")
	t.Setenv("LD_LIBRARY_PATH", wrong+string(os.PathListSeparator)+right)
	accept := func(path string) (bool, string) {
		if filepath.Dir(path) == wrong {
			return false, "built for FFmpeg 9"
		}
		return true, ""
	}
	got, err := findShimLibraryAccepting(accept)
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(right, "libffshim.so"); got != want {
		t.Errorf("found %s, want %s", got, want)
	}
}
