//go:build !ios && !android && (amd64 || arm64)

package avfilter

import (
	"fmt"
	"runtime"
	"sync"
	"testing"
)

// GraphCreateFilter's name and args are handed to C for the whole call: a
// buffersrc created on one goroutine read another's string (seen as
// "Error setting option width to value /tmp/..."), because the binding took
// them as uintptr, which keeps nothing alive.
func TestGraphCreateFilterKeepsItsStringsAlive(t *testing.T) {
	if err := Init(); err != nil {
		t.Skipf("no libavfilter: %v", err)
	}
	buffer := GetByName("buffer")
	if buffer == nil {
		t.Skip("no buffer filter")
	}
	var wg sync.WaitGroup
	errs := make(chan error, 64)
	for g := range 32 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range 50 {
				graph := GraphAlloc()
				args := fmt.Sprintf("video_size=%dx%d:pix_fmt=0:time_base=1/25:pixel_aspect=1/1", 64+g, 64+i)
				_, err := GraphCreateFilter(graph, buffer, fmt.Sprintf("in-%d-%d", g, i), args)
				GraphFree(&graph)
				if err != nil {
					errs <- err
					return
				}
				if i%5 == 0 {
					runtime.GC()
				}
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}
