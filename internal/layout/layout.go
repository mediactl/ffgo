//go:build !ios && !android && (amd64 || arm64)

// Package layout resolves FFmpeg struct offsets.
//
// ffgo reads FFmpeg structs at offsets its Go code states. Fields move
// between FFmpeg majors, and some go away (AVFrame.key_frame in FFmpeg 9).
// The shim, compiled against FFmpeg headers, knows the real offsets; layout
// uses them when the shim was compiled against the release that is loaded,
// and the Go value otherwise. Every lookup is registered, so Mismatches can
// audit every offset the Go code uses.
package layout

import (
	"fmt"
	"sort"
	"sync"

	"github.com/obinnaokechukwu/ffgo/internal/bindings"
	"github.com/obinnaokechukwu/ffgo/internal/shim"
)

var (
	mu         sync.Mutex
	registered = map[string]uintptr{} // field -> Go value
	optional   = map[string]bool{}    // fields some releases do not have

	once      sync.Once
	useShim   bool
	shimWhy   string // why the shim's offsets are not used
	shimLabel string
)

func decide() {
	once.Do(func() {
		_ = shim.Load()
		if err := bindings.Load(); err != nil {
			shimWhy = fmt.Sprintf("FFmpeg not loaded: %v", err)
			return
		}
		u, c, f, ok := shim.BuiltVersions()
		if !ok {
			shimWhy = "no shim, or a shim without the layout table"
			return
		}
		set, _ := bindings.LoadedVersionSet()
		if u != set.AVUtil || c != set.AVCodec || f != set.AVFormat {
			shimWhy = fmt.Sprintf("shim built for avutil %d, avcodec %d, avformat %d; FFmpeg %d is loaded (%d, %d, %d)",
				u, c, f, set.FFmpeg, set.AVUtil, set.AVCodec, set.AVFormat)
			return
		}
		useShim = true
		shimLabel = fmt.Sprintf("FFmpeg %d", set.FFmpeg)
	})
}

// ShimLoaded reports whether offsets come from the shim.
func ShimLoaded() bool { decide(); return useShim }

// Offset is field's offset ("AVCodecContext.width"): the shim's when the
// shim matches the loaded release and knows the field, else goValue.
func Offset(field string, goValue uintptr) uintptr {
	decide()
	mu.Lock()
	registered[field] = goValue
	mu.Unlock()
	if useShim {
		if off := shim.Offsetof(field); off >= 0 {
			return uintptr(off)
		}
	}
	return goValue
}

// OptionalOffset is Offset for a field some FFmpeg releases do not have;
// the audit does not report it as unknown. Callers check Known first.
func OptionalOffset(field string, goValue uintptr) uintptr {
	mu.Lock()
	optional[field] = true
	mu.Unlock()
	return Offset(field, goValue)
}

// Known reports whether the loaded release's headers have field, as far as
// the shim can say; without a usable shim it reports true, and callers
// fall back to what they did before the shim existed.
func Known(field string) bool {
	decide()
	if !useShim {
		return true
	}
	return shim.Offsetof(field) >= 0
}

// Registered is how many distinct fields the Go code has looked up.
func Registered() int { mu.Lock(); defer mu.Unlock(); return len(registered) }

// Mismatches lists every registered, non-optional field the shim does not
// know: an offset the Go code reads that the loaded release's headers have
// no such field for. Without a usable shim it says why, as its only entry.
func Mismatches() []string {
	decide()
	if !useShim {
		return []string{"offsets not audited: " + shimWhy}
	}
	mu.Lock()
	defer mu.Unlock()
	var out []string
	for field := range registered {
		if shim.Offsetof(field) < 0 && !optional[field] {
			out = append(out, fmt.Sprintf("%s: unknown to the %s headers", field, shimLabel))
		}
	}
	sort.Strings(out)
	return out
}

// Differences lists every registered field whose Go value differs from
// the shim's. The shim's value is the one used; the Go value is what a
// process without a matching shim would use.
func Differences() []string {
	decide()
	if !useShim {
		return nil
	}
	mu.Lock()
	defer mu.Unlock()
	var out []string
	for field, goValue := range registered {
		if off := shim.Offsetof(field); off >= 0 && uintptr(off) != goValue {
			out = append(out, fmt.Sprintf("%s: Go %d, %s headers %d", field, goValue, shimLabel, off))
		}
	}
	sort.Strings(out)
	return out
}
