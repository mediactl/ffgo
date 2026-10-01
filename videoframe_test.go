//go:build !ios && !android && (amd64 || arm64)

package ffgo

import "testing"

func TestNewVideoFrameEncodes(t *testing.T) {
	enc := x265Encoder(t, NewRational(1, 25), "")
	n := 0
	emit := func(*Packet) error { n++; return nil }
	for i := int64(0); i < 5; i++ {
		f, err := NewVideoFrame(PixelFormatYUV420P10LE(), 64, 64)
		if err != nil {
			t.Fatal(err)
		}
		if f.Format() != int32(PixelFormatYUV420P10LE()) {
			t.Fatalf("format %d", f.Format())
		}
		f.SetPTS(i)
		if err := enc.Encode(f, emit); err != nil {
			t.Fatal(err)
		}
		_ = f.Free()
	}
	if err := enc.Flush(emit); err != nil {
		t.Fatal(err)
	}
	if n != 5 {
		t.Errorf("%d packets, want 5", n)
	}
}
