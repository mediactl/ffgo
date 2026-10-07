//go:build !ios && !android && (amd64 || arm64)

package ffgo

import (
	"strings"
	"sync"
	"testing"

	"github.com/obinnaokechukwu/ffgo/internal/shim"
)

// av_log hands every line to a custom callback whatever its level (only
// av_log_default_callback filters), so before the shim filtered, every debug
// line cost a vsnprintf and a purego call.
func TestTheLogCallbackGetsOnlyLinesAtOrBelowTheLogLevel(t *testing.T) {
	requireShim(t)
	var mu sync.Mutex
	var got []string
	if err := SetLogCallback(func(_ LogLevel, msg string) {
		if !strings.HasPrefix(msg, "ffgo-level-test ") {
			return
		}
		mu.Lock()
		got = append(got, msg)
		mu.Unlock()
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = SetLogCallback(nil)
		_ = SetLogLevel(LogInfo)
	})
	if err := SetLogLevel(LogWarning); err != nil {
		t.Fatal(err)
	}
	for _, l := range []LogLevel{LogDebug, LogVerbose, LogInfo, LogWarning, LogError} {
		if err := shim.Log(nil, int32(l), "ffgo-level-test "+l.String()); err != nil {
			t.Fatal(err)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if want := "ffgo-level-test warning,ffgo-level-test error"; strings.Join(got, ",") != want {
		t.Fatalf("callback got %q, want %q", got, want)
	}
}
