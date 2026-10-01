//go:build !ios && !android && (amd64 || arm64)

package ffgo

import (
	"errors"
	"io"
	"math"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/obinnaokechukwu/ffgo/avcodec"
)

// toneOn makes a 1 s 48 kHz FLAC in layout whose only non-silent channel is ch.
func toneOn(t *testing.T, layout, ch string) string {
	t.Helper()
	ffmpegOrSkip(t)
	path := filepath.Join(t.TempDir(), "tone.mkv")
	run(t, "ffmpeg", "-hide_banner", "-loglevel", "error", "-y",
		"-f", "lavfi", "-i", "sine=frequency=1000:sample_rate=48000:duration=1",
		"-af", "pan="+layout+"|"+ch+"=c0", "-c:a", "flac", path)
	return path
}

// transcodeAudio decodes src's first audio stream, resamples it to layout
// at 48 kHz by layout name, encodes AAC at bitRate and muxes Matroska.
func transcodeAudio(t *testing.T, src, layout string, bitRate int64) string {
	t.Helper()
	d, err := NewDecoder(src)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	var in *StreamInfo
	for _, s := range d.Streams() {
		if s.Type == MediaTypeAudio {
			in = s
			break
		}
	}
	if in == nil {
		t.Fatal("no audio stream")
	}
	dec, err := d.NewStreamDecoder(in.Index, nil)
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
	enc, err := NewAudioEncoder(AudioEncoderConfig2{SampleRate: 48000, Layout: layout, BitRate: bitRate, GlobalHeader: true,
		InputTimeBase: dec.TimeBase()})
	if err != nil {
		t.Fatal(err)
	}
	defer enc.Close()
	ms := addEncoderStreamForTest(t, m, enc)
	if err := m.WriteHeader(); err != nil {
		t.Fatal(err)
	}

	var res *Resampler
	defer func() {
		if res != nil {
			res.Close()
		}
	}()
	emit := func(p *Packet) error { return m.WritePacket(ms, p) }
	encode := func(f Frame) {
		if res == nil {
			res, err = NewResampler(
				AudioFormat{SampleRate: in.SampleRate, Layout: in.ChannelLayout, SampleFormat: SampleFormat(f.Format())},
				AudioFormat{SampleRate: 48000, Layout: layout, SampleFormat: SampleFormatFLTP})
			if err != nil {
				t.Fatal(err)
			}
		}
		r, err := res.Resample(f)
		if err != nil {
			t.Fatal(err)
		}
		if r.IsNil() {
			return
		}
		r.SetPTS(f.PTS())
		if err := enc.Encode(r, emit); err != nil {
			t.Fatal(err)
		}
		_ = r.Free()
	}
	for done := false; !done; {
		p, err := d.ReadPacket()
		if err != nil {
			t.Fatal(err)
		}
		switch {
		case p == nil:
			err, done = dec.Send(nil), true
		case p.StreamIndex() == in.Index:
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
			encode(f)
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

var (
	astatsChannelRE = regexp.MustCompile(`Channel: (\d+)`)
	astatsRMSRE     = regexp.MustCompile(`RMS level dB: (\S+)`)
)

// channelRMSdB is each channel's RMS level in dB, by the name the layout
// gives its position (astats numbers channels from 1 in layout order).
func channelRMSdB(t *testing.T, path, layout string) map[string]float64 {
	t.Helper()
	names := map[string][]string{"5.1": {"FL", "FR", "FC", "LFE", "BL", "BR"}}[layout]
	out := run(t, "ffmpeg", "-hide_banner", "-nostats", "-i", path, "-af", "astats=measure_overall=none", "-f", "null", "-")
	levels := map[string]float64{}
	ch := 0
	for _, line := range strings.Split(out, "\n") {
		if m := astatsChannelRE.FindStringSubmatch(line); m != nil {
			ch, _ = strconv.Atoi(m[1])
		}
		if m := astatsRMSRE.FindStringSubmatch(line); m != nil && ch >= 1 && ch <= len(names) {
			v := math.Inf(-1)
			if m[1] != "-inf" {
				v, _ = strconv.ParseFloat(m[1], 64)
			}
			levels[names[ch-1]] = v
		}
	}
	if len(levels) != len(names) {
		t.Fatalf("astats gave %d channel levels, want %d:\n%s", len(levels), len(names), out)
	}
	return levels
}

func ffprobeLayout(t *testing.T, path string) string {
	t.Helper()
	return strings.TrimSpace(run(t, "ffprobe", "-v", "error", "-select_streams", "a:0",
		"-show_entries", "stream=channel_layout", "-of", "csv=p=0", path))
}

func TestSevenOneSideChannelsFoldIntoFiveOneSurrounds(t *testing.T) {
	requireShim(t)
	out := transcodeAudio(t, toneOn(t, "7.1", "SL"), "5.1", 384000)
	if got := ffprobeLayout(t, out); got != "5.1" {
		t.Fatalf("output layout %q, want 5.1", got)
	}
	l := channelRMSdB(t, out, "5.1")
	if l["BL"] < -30 { // the source tone is about -21 dB RMS; a -3 dB fold leaves about -24
		t.Errorf("BL at %.1f dB: SL did not fold into the left surround", l["BL"])
	}
	for _, ch := range []string{"FL", "FR", "FC", "BR"} {
		if l[ch] > -60 {
			t.Errorf("%s at %.1f dB: the SL tone leaked into it", ch, l[ch])
		}
	}
}

func TestSixChannelSideLayoutIsNotRemappedByCount(t *testing.T) {
	requireShim(t)
	out := transcodeAudio(t, toneOn(t, "5.1(side)", "SR"), "5.1", 384000)
	l := channelRMSdB(t, out, "5.1")
	if l["BR"] < -30 {
		t.Errorf("BR at %.1f dB: SR did not land in the right surround", l["BR"])
	}
	for _, ch := range []string{"FL", "FR", "FC", "BL"} {
		if l[ch] > -60 {
			t.Errorf("%s at %.1f dB: the SR tone was mapped there", ch, l[ch])
		}
	}
}

func TestAACEncoderTakesAnyFrameSize(t *testing.T) {
	requireShim(t)
	enc, err := NewAudioEncoder(AudioEncoderConfig2{SampleRate: 48000, Layout: "stereo", BitRate: 128000})
	if err != nil {
		t.Fatal(err)
	}
	defer enc.Close()
	if enc.FrameSize() != 1024 {
		t.Fatalf("AAC frame size %d, want 1024", enc.FrameSize())
	}
	packets := 0
	emit := func(*Packet) error { packets++; return nil }
	total := 0
	for _, n := range []int{4608, 333, 1, 2047, 4608} { // a FLAC block, odd sizes, one sample
		f := silentFrame(t, 48000, "stereo", n)
		f.SetPTS(int64(total))
		total += n
		if err := enc.Encode(f, emit); err != nil {
			t.Fatalf("encoding %d samples: %v", n, err)
		}
		_ = f.Free()
	}
	if err := enc.Flush(emit); err != nil {
		t.Fatal(err)
	}
	// One packet per 1024 samples (the last short), plus the encoder's priming frame.
	want := (total+1023)/1024 + 1
	if packets < want-1 || packets > want+1 {
		t.Errorf("%d packets for %d samples, want about %d", packets, total, want)
	}
}

// addEncoderStreamForTest adds a muxer stream for enc's packets. Until
// Muxer.AddEncoderStream exists (Task 6) it is a copy stream of the
// encoder's parameters, rescaled from the encoder's time base.
func addEncoderStreamForTest(t *testing.T, m *Muxer, enc *AudioEncoder) *MuxerStream {
	t.Helper()
	par, err := enc.Parameters()
	if err != nil {
		t.Fatal(err)
	}
	defer avcodec.ParametersFree(&par)
	ms, err := m.AddCopyStream(&CopyStreamConfig{CodecParameters: par, TimeBase: enc.TimeBase()})
	if err != nil {
		t.Fatal(err)
	}
	return ms
}

// silentFrame is an owned fltp frame of n silent samples in layout.
func silentFrame(t *testing.T, rate int, layout string, n int) Frame {
	t.Helper()
	f, err := NewAudioFrame(SampleFormatFLTP, rate, layout, n)
	if err != nil {
		t.Fatal(err)
	}
	return f
}
