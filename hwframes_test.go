//go:build !ios && !android && (amd64 || arm64)

package ffgo

import (
	"errors"
	"io"
	"testing"
	"unsafe"

	"github.com/obinnaokechukwu/ffgo/avutil"
	"github.com/obinnaokechukwu/ffgo/internal/shim"
)

// encodeThrough decodes clip on the CPU, runs filters on a graph holding
// dev, encodes with encoderName from the graph's GPU frames, and returns
// the number of packets.
func encodeThrough(t *testing.T, dev *HWDevice, clip, filters, encoderName string) int {
	t.Helper()
	d, err := NewDecoder(clip)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	vs := d.VideoStream()
	dec, err := d.NewStreamDecoder(vs.Index, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer dec.Close()
	g, err := NewFilterGraph(FilterGraphConfig{
		Width: vs.Width, Height: vs.Height, PixelFmt: vs.PixelFmt, TimeBase: dec.TimeBase(),
		Filters: filters, HWDevice: dev,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer g.Close()

	var enc *VideoStreamEncoder
	packets := 0
	emit := func(*Packet) error { packets++; return nil }
	encode := func(frames []*Frame) {
		for _, f := range frames {
			if enc == nil {
				enc, err = NewVideoStreamEncoder(VideoStreamEncoderConfig{
					VideoEncoderConfig: VideoEncoderConfig{
						Codec: CodecIDHEVC, EncoderName: encoderName, Width: vs.Width, Height: vs.Height,
						FrameRate: vs.FrameRate, HWFramesCtx: g.OutputHWFramesCtx(),
					},
					TimeBase: dec.TimeBase(),
				})
				if err != nil {
					t.Fatal(err)
				}
			}
			if err := enc.Encode(*f, emit); err != nil {
				t.Fatal(err)
			}
			_ = f.Free()
		}
	}
	for done := false; !done; {
		p, err := d.ReadPacket()
		if err != nil {
			t.Fatal(err)
		}
		switch {
		case p == nil:
			err, done = dec.Send(nil), true
		case p.StreamIndex() == vs.Index:
			err = dec.Send(p)
		default:
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		for {
			f, err := dec.Receive()
			if errors.Is(err, ErrAgain) || errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				t.Fatal(err)
			}
			out, err := g.Filter(&f)
			if err != nil {
				t.Fatal(err)
			}
			encode(out)
		}
	}
	out, err := g.Flush()
	if err != nil {
		t.Fatal(err)
	}
	encode(out)
	if enc == nil {
		t.Fatal("no frame reached the encoder")
	}
	defer enc.Close()
	if err := enc.Flush(emit); err != nil {
		t.Fatal(err)
	}
	return packets
}

func TestSoftwareDecodeUploadsThroughAFilterGraphDevice(t *testing.T) {
	// NVIDIA's path for a source NVDEC cannot decode: CPU decode, hwupload
	// (the graph's device), scale_cuda to 10-bit, hevc_nvenc.
	dev := cudaOrSkip(t)
	if n := encodeThrough(t, dev, h264Clip(t, 48), "hwupload,scale_cuda=format=p010le", "hevc_nvenc"); n != 48 {
		t.Errorf("%d packets, want 48", n)
	}
}

func TestNewHWFramesMakesAPoolOfTheAskedFormat(t *testing.T) {
	dev := cudaOrSkip(t)
	ref, err := NewHWFrames(HWFramesConfig{Device: dev, Format: PixelFormatCUDA(), SWFormat: PixelFormatP010LE(),
		Width: 256, Height: 144, PoolSize: 4})
	if err != nil {
		t.Fatal(err)
	}
	defer avutil.FreeBufferRef(&ref)
	if f, ok := shim.HWFramesFormat(unsafe.Pointer(ref)); !ok || PixelFormat(f) != PixelFormatCUDA() {
		t.Errorf("pool format %d (ok=%v), want CUDA %d", f, ok, PixelFormatCUDA())
	}
}
