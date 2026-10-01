//go:build !ios && !android && (amd64 || arm64)

package shim

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/obinnaokechukwu/ffgo/internal/bindings"
)

// A shim links the FFmpeg libraries of the headers it was compiled
// against (its DT_NEEDED sonames). Loaded beside another release it drags
// that release's libraries into the process, and its compiled accessors
// read the wrong offsets, so a shim is used only with its own release.
func TestShimMatchesOnlyItsOwnRelease(t *testing.T) {
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		t.Skip("the committed prebuilt checked here is linux-amd64")
	}
	prebuilt := filepath.Join("..", "..", "shim", "prebuilt", "linux-amd64", "libffshim.so")
	var nine, six bindings.VersionSet
	for _, s := range bindings.KnownVersionSets {
		switch s.FFmpeg {
		case 9:
			nine = s
		case 6:
			six = s
		}
	}
	if ok, why := shimMatchesRelease(prebuilt, nine); !ok {
		t.Fatalf("the FFmpeg 9 prebuilt does not match FFmpeg 9: %s", why)
	}
	ok, why := shimMatchesRelease(prebuilt, six)
	if ok {
		t.Fatal("the FFmpeg 9 prebuilt matched FFmpeg 6")
	}
	if !strings.Contains(why, "libavcodec.so.63") {
		t.Fatalf("reason does not name the mismatched library: %s", why)
	}
}
