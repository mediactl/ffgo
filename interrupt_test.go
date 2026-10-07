//go:build !ios && !android && (amd64 || arm64)

package ffgo

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// An empty file opened with the file protocol's follow=1 never reaches
// EOF: every read answers EAGAIN, and FFmpeg's I/O loop
// (retry_transfer_wrapper) polls the interrupt callback between retries --
// a stalled reader. A FIFO nobody writes would block inside open(2) instead,
// where FFmpeg never polls the callback.
func TestWithInterruptAbortsAnOpenOnAStalledReader(t *testing.T) {
	requireShim(t)
	path := filepath.Join(t.TempDir(), "stalled.mkv")
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	type result struct {
		d   *Decoder
		err error
	}
	res := make(chan result, 1)
	go func() {
		d, err := NewDecoder(path, WithAVOptions(map[string]string{"follow": "1"}), WithInterrupt(done))
		res <- result{d, err}
	}()
	select {
	case r := <-res:
		if r.d != nil {
			_ = r.d.Close()
		}
		t.Fatalf("the open returned before the interrupt: %v", r.err)
	case <-time.After(300 * time.Millisecond):
	}
	close(done)
	select {
	case r := <-res:
		if r.err == nil {
			_ = r.d.Close()
			t.Fatal("the open succeeded; want AVERROR_EXIT")
		}
		if !strings.Contains(r.err.Error(), "Immediate exit requested") {
			t.Errorf("err = %v, want AVERROR_EXIT (Immediate exit requested)", r.err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the open did not return within 5 s of the interrupt")
	}
	if n := interruptsRegistered(); n != 0 {
		t.Errorf("%d interrupt registrations left after a failed open, want 0", n)
	}
}

func TestADecoderWithAnInterruptUnregistersOnClose(t *testing.T) {
	requireShim(t)
	d, err := NewDecoder(gopClip(t, "mkv"), WithInterrupt(make(chan struct{})))
	if err != nil {
		t.Fatal(err)
	}
	if n := interruptsRegistered(); n != 1 {
		t.Fatalf("%d registrations while open, want 1", n)
	}
	_ = d.Close()
	if n := interruptsRegistered(); n != 0 {
		t.Fatalf("%d registrations after Close, want 0", n)
	}
}
