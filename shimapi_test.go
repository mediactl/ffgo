//go:build !ios && !android && (amd64 || arm64)

package ffgo

import "testing"

// The fork's shim reports the API version clustarr's pkg/ffruntime pins
// (MinShimAPI 1): an older shim would read FFmpeg 9 structs at old offsets.
func TestShimAPIVersionIsTheForks(t *testing.T) {
	requireShim(t)
	if got := ShimAPIVersion(); got != 2 {
		t.Fatalf("ShimAPIVersion() = %d, want 2 (FFSHIM_API_VERSION in shim/ffshim.h)", got)
	}
}
