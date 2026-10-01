//go:build !ios && !android && (amd64 || arm64)

package ffgo

import (
	"path/filepath"
	"strings"
	"testing"
)

// anamorphic is a 720x480 clip with a 32:27 sample aspect ratio (a DVD's
// 16:9), so it displays 853x480.
func anamorphic(t *testing.T) string {
	t.Helper()
	ffmpegOrSkip(t)
	path := filepath.Join(t.TempDir(), "dvd.mkv")
	run(t, "ffmpeg", "-hide_banner", "-loglevel", "error", "-y",
		"-f", "lavfi", "-i", "testsrc2=size=720x480:rate=25:duration=1,setsar=32/27",
		"-c:v", "mpeg2video", path)
	return path
}

func sarOf(t *testing.T, path string) string {
	return strings.Split(strings.TrimSpace(run(t, "ffprobe", "-v", "error", "-select_streams", "v:0",
		"-show_entries", "stream=sample_aspect_ratio", "-of", "csv=p=0", path)), ",")[0]
}

func TestStreamsReportTheSampleAspectRatio(t *testing.T) {
	requireShim(t)
	d, err := NewDecoder(anamorphic(t))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if got := d.Streams()[0].SampleAspectRatio; got != NewRational(32, 27) {
		t.Errorf("SampleAspectRatio %v, want 32/27", got)
	}
}

func TestACopiedStreamKeepsItsSampleAspectRatio(t *testing.T) {
	requireShim(t)
	src := anamorphic(t)
	d, err := NewDecoder(src)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	out := filepath.Join(t.TempDir(), "o.mkv")
	m, err := NewMuxer(out, "matroska")
	if err != nil {
		t.Fatal(err)
	}
	s := d.Streams()[0]
	ms, err := m.AddCopyStream(&CopyStreamConfig{CodecParameters: s.CodecParameters(), TimeBase: s.TimeBase})
	if err != nil {
		t.Fatal(err)
	}
	if err := m.WriteHeader(); err != nil {
		t.Fatal(err)
	}
	for {
		p, err := d.ReadPacket()
		if err != nil {
			t.Fatal(err)
		}
		if p == nil {
			break
		}
		c, _ := p.Clone()
		if err := m.WritePacket(ms, c); err != nil {
			t.Fatal(err)
		}
		_ = c.Free()
	}
	if err := m.WriteTrailer(); err != nil {
		t.Fatal(err)
	}
	_ = m.Close()
	if got := sarOf(t, out); got != "32:27" {
		t.Errorf("copied stream SAR %q, want 32:27", got)
	}
}

func TestAnEncodedStreamKeepsTheSampleAspectRatio(t *testing.T) {
	requireShim(t)
	enc, err := NewVideoStreamEncoder(VideoStreamEncoderConfig{
		VideoEncoderConfig: VideoEncoderConfig{EncoderName: "libx265", Width: 720, Height: 480,
			PixelFormat: PixelFormatYUV420P10LE(), FrameRate: NewRational(25, 1), SampleAspectRatio: NewRational(32, 27),
			CodecOptions: map[string]string{"x265-params": "log-level=error", "preset": "ultrafast"}},
		TimeBase: NewRational(1, 25), GlobalHeader: true,
	})
	if err != nil {
		t.Skipf("no libx265: %v", err)
	}
	defer enc.Close()
	out := filepath.Join(t.TempDir(), "o.mkv")
	m, err := NewMuxer(out, "matroska")
	if err != nil {
		t.Fatal(err)
	}
	ms, err := m.AddEncoderStream(enc, StreamOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := m.WriteHeader(); err != nil {
		t.Fatal(err)
	}
	emit := func(p *Packet) error { return m.WritePacket(ms, p) }
	for i := int64(0); i < 5; i++ {
		f, err := NewVideoFrame(PixelFormatYUV420P10LE(), 720, 480)
		if err != nil {
			t.Fatal(err)
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
	if err := m.WriteTrailer(); err != nil {
		t.Fatal(err)
	}
	_ = m.Close()
	if got := sarOf(t, out); got != "32:27" {
		t.Errorf("encoded stream SAR %q, want 32:27", got)
	}
}
