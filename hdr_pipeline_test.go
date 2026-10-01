//go:build !ios && !android && (amd64 || arm64)

package ffgo

import (
	"errors"
	"io"
	"path/filepath"
	"strings"
	"testing"
)

// hdrRun configures hdr10Transcode.
type hdrRun struct {
	encoder string      // "libx265" or "hevc_nvenc"
	device  *HWDevice   // CUDA: NVDEC decode, filter on the GPU
	filter  string      // GPU filter, e.g. "scale_cuda=format=p010le"
	mutate  func(Frame) // called on each decoded frame before side data is removed
	atEnc   func(Frame) // called on each frame as it reaches the encoder
}

// hdr10Transcode is the Engine's video leg in miniature: decode, remove
// HDR10+ and Dolby Vision side data from every frame, encode HEVC Main 10
// keeping the source's mastering display and light level (as encoder side
// data and as output stream side data) and its colour tags, mux Matroska.
func hdr10Transcode(t *testing.T, src string, r hdrRun) string {
	t.Helper()
	d, err := NewDecoder(src)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	vs := d.VideoStream()
	dec, err := d.NewStreamDecoder(vs.Index, &StreamDecoderConfig{HWDevice: r.device, ExtraHWFrames: 16})
	if err != nil {
		t.Fatal(err)
	}
	defer dec.Close()

	out := filepath.Join(t.TempDir(), "out.mkv")
	m, err := NewMuxer(out, "matroska")
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()

	var (
		enc   *VideoStreamEncoder
		graph *FilterGraph
		ms    *MuxerStream
	)
	defer func() {
		if enc != nil {
			enc.Close()
		}
		if graph != nil {
			graph.Close()
		}
	}()
	emit := func(p *Packet) error { return m.WritePacket(ms, p) }

	// open creates the encoder and the output stream from the first frame,
	// which carries the HDR10 side data when the container does not.
	open := func(first Frame) {
		md, okMD := StreamSideData(vs.CodecParameters(), PacketSideMasteringDisplay())
		cll, okCLL := StreamSideData(vs.CodecParameters(), PacketSideContentLightLevel())
		if !okMD {
			md, okMD = first.SideData(FrameSideMasteringDisplay())
		}
		if !okCLL {
			cll, okCLL = first.SideData(FrameSideContentLightLevel())
		}
		encSide := map[FrameSideDataType][]byte{}
		streamSide := map[PacketSideDataType][]byte{}
		if okMD {
			encSide[FrameSideMasteringDisplay()] = md
			streamSide[PacketSideMasteringDisplay()] = md
		}
		if okCLL {
			encSide[FrameSideContentLightLevel()] = cll
			streamSide[PacketSideContentLightLevel()] = cll
		}
		cfg := VideoEncoderConfig{
			Codec: CodecIDHEVC, EncoderName: r.encoder, Width: vs.Width, Height: vs.Height,
			FrameRate: vs.FrameRate, PixelFormat: PixelFormatYUV420P10LE(),
			CodecOptions: map[string]string{
				"color_primaries": "bt2020", "color_trc": "smpte2084", "colorspace": "bt2020nc", "color_range": "tv",
			},
		}
		if r.encoder == "libx265" {
			cfg.CodecOptions["x265-params"] = "log-level=error"
		}
		if graph != nil {
			cfg.HWFramesCtx = graph.OutputHWFramesCtx()
		}
		enc, err = NewVideoStreamEncoder(VideoStreamEncoderConfig{
			VideoEncoderConfig: cfg, TimeBase: dec.TimeBase(), SideData: encSide, GlobalHeader: m.NeedsGlobalHeader(),
		})
		if errors.Is(err, ErrNotSupported) {
			t.Skipf("encoder side data needs FFmpeg 7: %v", err)
		}
		if err != nil {
			t.Fatal(err)
		}
		if ms, err = m.AddEncoderStream(enc, StreamOptions{SideData: streamSide}); errors.Is(err, ErrNotSupported) {
			t.Skipf("stream side data needs FFmpeg 6.1: %v", err)
		} else if err != nil {
			t.Fatal(err)
		}
		if err := m.WriteHeader(); err != nil {
			t.Fatal(err)
		}
	}
	encode := func(f Frame) {
		f.RemoveSideData(FrameSideHDRPlus(), FrameSideDOVIRPU(), FrameSideDOVIMetadata())
		if r.atEnc != nil {
			r.atEnc(f)
		}
		if err := enc.Encode(f, emit); err != nil {
			t.Fatal(err)
		}
	}
	handle := func(f Frame) {
		if r.mutate != nil {
			r.mutate(f)
		}
		if r.filter == "" {
			if enc == nil {
				open(f)
			}
			encode(f)
			return
		}
		if graph == nil {
			if graph, err = NewFilterGraph(FilterGraphConfig{
				Width: vs.Width, Height: vs.Height, PixelFmt: PixelFormat(f.Format()), TimeBase: dec.TimeBase(),
				Filters: r.filter, HWFramesCtx: f.HWFramesCtx(),
			}); err != nil {
				t.Fatal(err)
			}
		}
		outs, err := graph.Filter(&f)
		if err != nil {
			t.Fatal(err)
		}
		for _, o := range outs {
			if enc == nil {
				open(*o)
			}
			encode(*o)
			_ = o.Free()
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
			handle(f)
		}
	}
	if graph != nil {
		outs, err := graph.Flush()
		if err != nil {
			t.Fatal(err)
		}
		for _, o := range outs {
			encode(*o)
			_ = o.Free()
		}
	}
	if err := enc.Flush(emit); err != nil {
		t.Fatal(err)
	}
	if err := m.WriteTrailer(); err != nil {
		t.Fatal(err)
	}
	return out
}

func ffprobeEntries(t *testing.T, path string, args ...string) string {
	t.Helper()
	return run(t, "ffprobe", append(append([]string{"-v", "error", "-select_streams", "v:0"}, args...), "-of", "default=nw=1", path)...)
}

func TestHDR10SurvivesALibx265ReEncode(t *testing.T) {
	requireShim(t)
	out := hdr10Transcode(t, hdr10Clip(t), hdrRun{encoder: "libx265"})
	stream := ffprobeEntries(t, out, "-show_entries", "stream_side_data=side_data_type")
	for _, want := range []string{"Mastering display metadata", "Content light level metadata"} {
		if !strings.Contains(stream, want) {
			t.Errorf("output stream side data lacks %q:\n%s", want, stream)
		}
	}
	frame := ffprobeEntries(t, out, "-read_intervals", "%+#1", "-show_entries", "frame_side_data=side_data_type")
	if !strings.Contains(frame, "Mastering display metadata") {
		t.Errorf("first frame lacks in-band mastering display SEI:\n%s", frame)
	}
	props := ffprobeEntries(t, out, "-show_entries", "stream=color_transfer,pix_fmt,profile")
	for _, want := range []string{"color_transfer=smpte2084", "pix_fmt=yuv420p10le", "profile=Main 10"} {
		if !strings.Contains(props, want) {
			t.Errorf("output lacks %s:\n%s", want, props)
		}
	}
}

func TestHDR10SurvivesAnNVENCReEncode(t *testing.T) {
	requireShim(t)
	dev := cudaOrSkip(t)
	out := hdr10Transcode(t, hdr10Clip(t), hdrRun{encoder: "hevc_nvenc", device: dev, filter: "scale_cuda=format=p010le"})
	stream := ffprobeEntries(t, out, "-show_entries", "stream_side_data=side_data_type")
	for _, want := range []string{"Mastering display metadata", "Content light level metadata"} {
		if !strings.Contains(stream, want) {
			t.Errorf("output stream side data lacks %q:\n%s", want, stream)
		}
	}
	props := ffprobeEntries(t, out, "-show_entries", "stream=color_transfer,profile")
	for _, want := range []string{"color_transfer=smpte2084", "profile=Main 10"} {
		if !strings.Contains(props, want) {
			t.Errorf("output lacks %s:\n%s", want, props)
		}
	}
	t.Logf("NVENC first-frame side data (in-band SEI): %q",
		ffprobeEntries(t, out, "-read_intervals", "%+#1", "-show_entries", "frame_side_data=side_data_type"))
}

func TestHDRPlusIsRemovedFromEveryFrame(t *testing.T) {
	requireShim(t)
	frames := 0
	hdr10Transcode(t, hdr10Clip(t), hdrRun{
		encoder: "libx265",
		mutate: func(f Frame) {
			if err := f.AddSideData(FrameSideHDRPlus(), []byte{1}); err != nil {
				t.Fatal(err)
			}
		},
		atEnc: func(f Frame) {
			frames++
			if _, ok := f.SideData(FrameSideHDRPlus()); ok {
				t.Errorf("frame %d reached the encoder with HDR10+ side data", frames)
			}
		},
	})
	if frames != 24 {
		t.Errorf("%d frames reached the encoder, want 24", frames)
	}
}
