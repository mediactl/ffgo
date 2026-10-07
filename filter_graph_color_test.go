//go:build !ios && !android && (amd64 || arm64)

package ffgo

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestVideoBufferArgs(t *testing.T) {
	base := FilterGraphConfig{Width: 128, Height: 72, PixelFmt: PixelFormat(0), TimeBase: NewRational(1, 1000), SAR: NewRational(1, 1)}
	if got, want := videoBufferArgs(base), "video_size=128x72:pix_fmt=0:time_base=1/1000:pixel_aspect=1/1"; got != want {
		t.Errorf("no colour: got %q, want %q", got, want)
	}
	c := base
	c.ColorSpace, c.ColorRange = ColorSpace(1), ColorRangeMPEG
	if got, want := videoBufferArgs(c), "video_size=128x72:pix_fmt=0:time_base=1/1000:pixel_aspect=1/1:colorspace=1:range=1"; got != want {
		t.Errorf("BT.709 limited: got %q, want %q", got, want)
	}
}

// scale's YUV-to-RGB matrix and range follow the buffer source, which
// ffmpeg(1) configures from the first frame's colorspace and range.
func TestAVideoGraphConvertsWithTheFramesColorspace(t *testing.T) {
	requireShim(t)
	path := gopClip(t, "mkv") // BT.709, limited range
	ref := filepath.Join(t.TempDir(), "ref.rgb")
	run(t, "ffmpeg", "-hide_banner", "-loglevel", "error", "-y", "-i", path, "-map", "0:v:0", "-frames:v", "1",
		"-sws_flags", "area", "-vf", "scale=64:36:flags=area", "-pix_fmt", "rgb24", "-f", "rawvideo", ref)
	want, err := os.ReadFile(ref)
	if err != nil {
		t.Fatal(err)
	}
	if got := firstRGBFrame(t, path, true, false); !bytes.Equal(got, want) {
		t.Fatalf("first RGB frame differs from ffmpeg's (%d vs %d bytes)", len(got), len(want))
	}
	// On FFmpeg 9 scale also reads a tagged frame's own colorspace, so the
	// buffer source's is what decides only for a frame without tags: given
	// through the config it still converts as BT.709, and without it scale
	// falls back to BT.601.
	if got := firstRGBFrame(t, path, true, true); !bytes.Equal(got, want) {
		t.Fatal("an untagged frame with the colorspace in the config differs from ffmpeg's BT.709 conversion")
	}
	if got := firstRGBFrame(t, path, false, true); bytes.Equal(got, want) {
		t.Fatal("an untagged frame without a configured colorspace still converts as BT.709: the fixture does not exercise the buffer source's colorspace")
	}
}

// firstRGBFrame scales the first decoded frame of path to 64x36 RGB24 through
// a filter graph, giving the graph the frame's colorspace and range when
// withColor, and clearing the frame's own colour tags first when untag.
func firstRGBFrame(t *testing.T, path string, withColor, untag bool) []byte {
	t.Helper()
	d, err := NewDecoder(path)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	si := d.VideoStream()
	sd, err := d.NewStreamDecoder(si.Index, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer sd.Close()
	var got []byte
	decodeStream(t, d, sd, func(f Frame) {
		if got != nil {
			return
		}
		w := WrapFrame(f, MediaTypeVideo)
		cs := f.ColorSpec()
		cfg := FilterGraphConfig{
			Width: w.Width(), Height: w.Height(), PixelFmt: PixelFormat(f.Format()),
			TimeBase: sd.TimeBase(), SAR: si.SampleAspectRatio,
			Filters: "scale=64:36:flags=area,format=pix_fmts=rgb24",
		}
		if withColor {
			cfg.ColorSpace, cfg.ColorRange = cs.Space, cs.Range
		}
		if untag {
			f.SetColorSpec(ColorSpec{Range: ColorRangeUnspecified, Space: 2, Primaries: 2, Transfer: 2}) // AVCOL_*_UNSPECIFIED
		}
		g, err := NewFilterGraph(cfg)
		if err != nil {
			t.Fatal(err)
		}
		defer g.Close()
		out, err := g.Filter(&f)
		if err != nil || len(out) == 0 {
			t.Fatalf("filter: %d frames, %v", len(out), err)
		}
		o := WrapFrame(*out[0], MediaTypeVideo)
		got = []byte{}
		for y := 0; y < 36; y++ {
			got = append(got, o.Data(0)[y*o.Linesize(0):y*o.Linesize(0)+64*3]...)
		}
		for _, fr := range out {
			_ = fr.Free()
		}
	})
	return got
}
