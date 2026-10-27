//go:build !ios && !android && (amd64 || arm64)

package ffgo

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// The AC-3 decoder attaches the bitstream's downmix levels to every frame
// (AV_FRAME_DATA_DOWNMIX_INFO); aresample reads them from the buffer source
// at init, so ffmpeg(1) hands the first frame's side data to abuffer.
// Without it the mono downmix uses swresample's -3 dB defaults.
func TestInputSideDataReachesAresampleAsInFFmpeg(t *testing.T) {
	requireShim(t)
	ffmpegOrSkip(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "mix.mka")
	run(t, "ffmpeg", "-hide_banner", "-loglevel", "error", "-y", "-f", "lavfi",
		"-i", "sine=frequency=440:duration=2:sample_rate=48000,aformat=channel_layouts=5.1(side)",
		"-c:a", "ac3", "-center_mixlev", "0.5", "-surround_mixlev", "0.5", path)
	ref := filepath.Join(dir, "ref.pcm")
	run(t, "ffmpeg", "-hide_banner", "-loglevel", "error", "-y", "-i", path, "-map", "0:a:0",
		"-ac", "1", "-ar", "11025", "-f", "s16le", ref)
	want, err := os.ReadFile(ref)
	if err != nil {
		t.Fatal(err)
	}
	if got := downmixPCM(t, path, true); !bytes.Equal(got, want) {
		t.Fatalf("with the first frame's side data: %d bytes, ffmpeg %d, contents differ", len(got), len(want))
	}
	if got := downmixPCM(t, path, false); bytes.Equal(got, want) {
		t.Fatal("without side data the downmix still equals ffmpeg's: the fixture does not exercise the downmix levels")
	}
}

// downmixPCM decodes path's audio through the chain `ffmpeg -ac 1 -ar 11025
// -f s16le` builds, giving the buffer source the first frame's side data
// when withSideData.
func downmixPCM(t *testing.T, path string, withSideData bool) []byte {
	t.Helper()
	d, err := NewDecoder(path)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	sd, err := d.NewStreamDecoder(d.AudioStream().Index, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer sd.Close()
	var g *FilterGraph
	var pcm []byte
	take := func(out []*Frame) {
		for _, o := range out {
			w := WrapFrame(*o, MediaTypeAudio)
			pcm = append(pcm, w.Data(0)[:2*w.NumSamples()]...)
			_ = o.Free()
		}
	}
	decodeStream(t, d, sd, func(f Frame) {
		if g == nil {
			rate := WrapFrame(f, MediaTypeAudio).SampleRate()
			cfg := FilterGraphConfig{
				SampleRate: rate, SampleFmt: SampleFormat(f.Format()), Layout: f.ChannelLayout(),
				TimeBase: NewRational(1, int32(rate)),
				Filters:  "aformat=sample_fmts=s16:sample_rates=11025:channel_layouts=mono",
			}
			if withSideData {
				cfg.InputSideData = &f
			}
			var err error
			if g, err = NewFilterGraph(cfg); errors.Is(err, ErrNotSupported) {
				t.Skipf("side data on a buffer source needs FFmpeg 7.1: %v", err)
			} else if err != nil {
				t.Fatal(err)
			}
		}
		out, err := g.Filter(&f)
		if err != nil {
			t.Fatal(err)
		}
		take(out)
	})
	out, err := g.Flush()
	if err != nil {
		t.Fatal(err)
	}
	take(out)
	_ = g.Close()
	return pcm
}
