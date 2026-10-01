//go:build !ios && !android && (amd64 || arm64)

package ffgo

import (
	"path/filepath"
	"testing"
)

// The names ffprobe prints, from FFmpeg itself: the demuxer, a pixel
// format, and a frame's colour description.
func TestNamesAsFFprobePrintsThem(t *testing.T) {
	ffmpegOrSkip(t)
	requireShim(t)
	path := filepath.Join(t.TempDir(), "hdr.mkv")
	run(t, "ffmpeg", "-hide_banner", "-loglevel", "error", "-y",
		"-f", "lavfi", "-i", "testsrc2=size=64x64:rate=25:duration=1,format=yuv420p10le,setparams=color_primaries=bt2020:color_trc=smpte2084:colorspace=bt2020nc:range=tv",
		"-c:v", "ffv1", path)
	d, err := NewDecoder(path)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if got := d.FormatName(); got != "matroska,webm" {
		t.Errorf("FormatName %q", got)
	}
	if got := PixelFormatName(PixelFormatYUV420P10LE()); got != "yuv420p10le" {
		t.Errorf("PixelFormatName %q", got)
	}
	f, err := d.DecodeVideo()
	if err != nil || f.IsNil() {
		t.Fatalf("decode: %v", err)
	}
	p, trc, sp, rng := f.ColorSpec().Names()
	if p != "bt2020" || trc != "smpte2084" || sp != "bt2020nc" || rng != "tv" {
		t.Errorf("colour names %q %q %q %q", p, trc, sp, rng)
	}
}

// A stream's bit rate is its codec parameters', as ffprobe prints
// bit_rate: AC-3's is in its header, 448 kb/s at 5.1 by default.
func TestStreamsReportTheirBitRate(t *testing.T) {
	ffmpegOrSkip(t)
	requireShim(t)
	path := filepath.Join(t.TempDir(), "ac3.mkv")
	run(t, "ffmpeg", "-hide_banner", "-loglevel", "error", "-y",
		"-f", "lavfi", "-i", "sine=duration=1,aformat=channel_layouts=5.1", "-c:a", "ac3", "-b:a", "448k", path)
	d, err := NewDecoder(path)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if got := d.Streams()[0].BitRate; got != 448000 {
		t.Errorf("BitRate %d, want 448000", got)
	}
}
