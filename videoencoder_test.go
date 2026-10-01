//go:build !ios && !android && (amd64 || arm64)

package ffgo

import (
	"errors"
	"slices"
	"testing"

	"github.com/obinnaokechukwu/ffgo/avutil"
)

// x265Encoder is a 64x64 yuv420p10le libx265 encoder at time base tb.
func x265Encoder(t *testing.T, tb Rational, x265Params string) *VideoStreamEncoder {
	t.Helper()
	if err := Init(); err != nil {
		t.Skipf("FFmpeg libraries not available: %v", err)
	}
	params := "log-level=error"
	if x265Params != "" {
		params += ":" + x265Params
	}
	enc, err := NewVideoStreamEncoder(VideoStreamEncoderConfig{
		VideoEncoderConfig: VideoEncoderConfig{
			Codec: CodecIDHEVC, EncoderName: "libx265", Width: 64, Height: 64,
			PixelFormat: PixelFormatYUV420P10LE(), FrameRate: NewRational(tb.Den, tb.Num),
			CodecOptions: map[string]string{"x265-params": params},
		},
		TimeBase: tb,
	})
	if errors.Is(err, ErrEncoderNotFound) {
		t.Skip("no libx265 in this FFmpeg")
	}
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = enc.Close() })
	return enc
}

// grayFrame is an owned w x h frame of pf with allocated buffers.
func grayFrame(t *testing.T, w, h int, pf PixelFormat) Frame {
	t.Helper()
	f := FrameAlloc()
	avutil.SetFrameWidth(f.ptr, int32(w))
	avutil.SetFrameHeight(f.ptr, int32(h))
	avutil.SetFrameFormat(f.ptr, int32(pf))
	if err := avutil.FrameGetBufferErr(f.ptr, 0); err != nil {
		t.Fatal(err)
	}
	return f
}

func TestVideoStreamEncoderKeepsFramePTS(t *testing.T) {
	enc := x265Encoder(t, NewRational(1001, 24000), "bframes=0")
	var pts []int64
	emit := func(p *Packet) error { pts = append(pts, p.PTS()); return nil }
	for i := int64(0); i < 10; i++ {
		f := grayFrame(t, 64, 64, PixelFormatYUV420P10LE())
		f.SetPTS(1000 + i) // as after a seek: not starting at zero
		if err := enc.Encode(f, emit); err != nil {
			t.Fatal(err)
		}
		_ = f.Free()
	}
	if err := enc.Flush(emit); err != nil {
		t.Fatal(err)
	}
	slices.Sort(pts)
	want := []int64{1000, 1001, 1002, 1003, 1004, 1005, 1006, 1007, 1008, 1009}
	if !slices.Equal(pts, want) {
		t.Errorf("packet PTS %v, want %v", pts, want)
	}
	if tb := enc.TimeBase(); tb != NewRational(1001, 24000) {
		t.Errorf("time base %v, want 1001/24000", tb)
	}
}

func TestVideoStreamEncoderNeverDropsAFrameUnderBackPressure(t *testing.T) {
	enc := x265Encoder(t, NewRational(1, 25), "rc-lookahead=40:frame-threads=4")
	n := 0
	emit := func(*Packet) error { n++; return nil }
	for i := int64(0); i < 120; i++ {
		f := grayFrame(t, 64, 64, PixelFormatYUV420P10LE())
		f.SetPTS(i)
		if err := enc.Encode(f, emit); err != nil {
			t.Fatal(err)
		}
		_ = f.Free()
	}
	if err := enc.Flush(emit); err != nil {
		t.Fatal(err)
	}
	if n != 120 {
		t.Errorf("%d packets for 120 frames", n)
	}
}

func TestEncoderNameMustEncodeTheCodec(t *testing.T) {
	if err := Init(); err != nil {
		t.Skip(err)
	}
	_, err := NewVideoStreamEncoder(VideoStreamEncoderConfig{
		VideoEncoderConfig: VideoEncoderConfig{Codec: CodecIDH264, EncoderName: "libx265", Width: 64, Height: 64},
		TimeBase:           NewRational(1, 25),
	})
	if !errors.Is(err, ErrEncoderCodecMismatch) {
		t.Errorf("err = %v, want ErrEncoderCodecMismatch", err)
	}
}

func TestUnknownCodecOptionIsAnError(t *testing.T) {
	// NVENC ignores -cq under constqp and FFmpeg's CLI says nothing; an
	// option the encoder does not have must not be dropped silently either.
	if err := Init(); err != nil {
		t.Skip(err)
	}
	_, err := NewVideoStreamEncoder(VideoStreamEncoderConfig{
		VideoEncoderConfig: VideoEncoderConfig{
			Codec: CodecIDHEVC, EncoderName: "libx265", Width: 64, Height: 64, PixelFormat: PixelFormatYUV420P10LE(),
			CodecOptions: map[string]string{"no-such-option": "1"},
		},
		TimeBase: NewRational(1, 25),
	})
	if err == nil {
		t.Fatal("an unknown codec option was accepted")
	}
}
