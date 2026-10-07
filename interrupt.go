//go:build !ios && !android && (amd64 || arm64)

package ffgo

import (
	"sync"

	"github.com/ebitengine/purego"
)

// One process-wide purego callback (purego never frees one, so never one per
// open) looks its opaque id up in this registry.
var (
	interruptMu     sync.Mutex
	interruptNext   uintptr
	interrupts      = map[uintptr]<-chan struct{}{}
	interruptCBOnce sync.Once
	interruptCB     uintptr
)

func interruptCallback() uintptr {
	interruptCBOnce.Do(func() { interruptCB = purego.NewCallback(interruptTrampoline) })
	return interruptCB
}

// interruptTrampoline is FFmpeg's int (*)(void *opaque): 1 aborts the I/O.
func interruptTrampoline(opaque uintptr) uintptr {
	interruptMu.Lock()
	done := interrupts[opaque]
	interruptMu.Unlock()
	if done == nil {
		return 0
	}
	select {
	case <-done:
		return 1
	default:
		return 0
	}
}

func registerInterrupt(done <-chan struct{}) uintptr {
	interruptMu.Lock()
	defer interruptMu.Unlock()
	interruptNext++
	interrupts[interruptNext] = done
	return interruptNext
}

func unregisterInterrupt(id uintptr) {
	if id == 0 {
		return
	}
	interruptMu.Lock()
	delete(interrupts, id)
	interruptMu.Unlock()
}

func interruptsRegistered() int {
	interruptMu.Lock()
	defer interruptMu.Unlock()
	return len(interrupts)
}

// WithInterrupt makes FFmpeg abort the decoder's I/O with AVERROR_EXIT once
// done is closed: the open, the stream-info read and every packet read. It
// acts where FFmpeg's I/O loops poll (EAGAIN retries, network protocols), not
// inside a read(2) blocked in the kernel. It needs the shim.
func WithInterrupt(done <-chan struct{}) DecoderOption {
	return func(o *DecoderOptions) { o.Interrupt = done }
}
