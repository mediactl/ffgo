//go:build !ios && !android && (amd64 || arm64)

package shim

import "testing"

// A shim built before ffshim_api_version existed reports 0, which callers
// pinning a minimum refuse.
func TestAPIVersionIsZeroWithoutTheSymbol(t *testing.T) {
	saved := shimAPIVersion
	shimAPIVersion = nil
	t.Cleanup(func() { shimAPIVersion = saved })
	if got := APIVersion(); got != 0 {
		t.Fatalf("APIVersion() without the symbol = %d, want 0", got)
	}
}
