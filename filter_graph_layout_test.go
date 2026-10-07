//go:build !ios && !android && (amd64 || arm64)

package ffgo

import (
	"path/filepath"
	"testing"
)

func TestAudioBufferArgs(t *testing.T) {
	for _, tc := range []struct {
		name string
		cfg  FilterGraphConfig
		want string
	}{
		{"named layout", FilterGraphConfig{SampleRate: 48000, SampleFmt: SampleFormatFLTP, Layout: "5.1(side)"},
			"sample_rate=48000:sample_fmt=fltp:channel_layout=5.1(side)"},
		{"unspecified order", FilterGraphConfig{SampleRate: 48000, SampleFmt: SampleFormatS16, Layout: "3 channels"},
			"sample_rate=48000:sample_fmt=s16:channels=3"},
		{"time base", FilterGraphConfig{SampleRate: 44100, SampleFmt: SampleFormatFLTP, Layout: "stereo", TimeBase: NewRational(1, 44100)},
			"time_base=1/44100:sample_rate=44100:sample_fmt=fltp:channel_layout=stereo"},
		{"channel count only, as before", FilterGraphConfig{SampleRate: 48000, SampleFmt: SampleFormatS16, Channels: 6},
			"sample_rate=48000:sample_fmt=s16:channel_layout=5.1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := audioBufferArgs(tc.cfg); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

// Before Layout, a 5.1(side) frame could not enter an audio graph: six
// channels became "5.1", and abuffer refused the side layout.
func TestAnAudioGraphTakesTheFramesOwnLayout(t *testing.T) {
	requireShim(t)
	ffmpegOrSkip(t)
	path := filepath.Join(t.TempDir(), "side.mka")
	run(t, "ffmpeg", "-hide_banner", "-loglevel", "error", "-y", "-f", "lavfi",
		"-i", "sine=frequency=440:duration=1:sample_rate=48000,aformat=channel_layouts=5.1(side)", "-c:a", "flac", path)
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
	samples := 0
	take := func(out []*Frame) {
		for _, o := range out {
			samples += WrapFrame(*o, MediaTypeAudio).NumSamples()
			_ = o.Free()
		}
	}
	decodeStream(t, d, sd, func(f Frame) {
		if g == nil {
			rate := WrapFrame(f, MediaTypeAudio).SampleRate()
			var err error
			g, err = NewFilterGraph(FilterGraphConfig{
				SampleRate: rate, SampleFmt: SampleFormat(f.Format()), Layout: f.ChannelLayout(),
				TimeBase: NewRational(1, int32(rate)),
				Filters:  "aformat=sample_fmts=s16:sample_rates=11025:channel_layouts=mono",
			})
			if err != nil {
				t.Fatalf("graph for a %s frame: %v", f.ChannelLayout(), err)
			}
		}
		out, err := g.Filter(&f)
		if err != nil {
			t.Fatal(err)
		}
		take(out)
	})
	if g == nil {
		t.Fatal("no audio frame decoded")
	}
	out, err := g.Flush()
	if err != nil {
		t.Fatal(err)
	}
	take(out)
	_ = g.Close()
	if samples < 11025-16 || samples > 11025+16 {
		t.Fatalf("got %d mono samples at 11025 Hz for 1 s of 5.1(side) audio, want 11025", samples)
	}
}
