//go:build !ios && !android && (amd64 || arm64)

package ffgo

import (
	"bytes"
	"encoding/binary"
	"errors"
	"math"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/obinnaokechukwu/ffgo/avdevice"
)

// The HDR10 side data the clip carries, in FFmpeg's layouts.
// AVMasteringDisplayMetadata: display_primaries[3][2] (R, G, B; x then y),
// white_point[2], min_luminance, max_luminance as AVRationals, then
// has_primaries and has_luminance. AVContentLightMetadata: MaxCLL, MaxFALL.
var (
	// Numerators over 50000 (chromaticities) and 10000 (luminance).
	needsPrimaries = [5][2]int32{
		{34000, 16000}, // R 0.680/0.320
		{13250, 34500}, // G 0.265/0.690
		{7500, 3000},   // B 0.150/0.060
		{15635, 16450}, // white point 0.3127/0.3290
		{50, 10000000}, // min 0.0050 and max 1000 cd/m²
	}
	needsMaxCLL, needsMaxFALL uint32 = 1000, 400
)

func masteringDisplayBlob() []byte {
	b := make([]byte, 0, 88)
	put := func(num, den int32) {
		b = binary.LittleEndian.AppendUint32(b, uint32(num))
		b = binary.LittleEndian.AppendUint32(b, uint32(den))
	}
	for i := 0; i < 4; i++ {
		put(needsPrimaries[i][0], 50000)
		put(needsPrimaries[i][1], 50000)
	}
	put(needsPrimaries[4][0], 10000) // min_luminance
	put(needsPrimaries[4][1], 10000) // max_luminance
	b = binary.LittleEndian.AppendUint32(b, 1)
	return binary.LittleEndian.AppendUint32(b, 1)
}

func lightLevelBlob() []byte {
	b := binary.LittleEndian.AppendUint32(nil, needsMaxCLL)
	return binary.LittleEndian.AppendUint32(b, needsMaxFALL)
}

// What clustarr's internal/testmedia builds clips from, without the CLI:
// lavfi sources as `-f lavfi -i`, an HEVC Main 10 stream carrying HDR10
// mastering-display and light-level side data, an AC-3 track with mix
// levels, two chapters and a format tag -- each read back through ffgo.
func TestTestmediaNeedsRoundTrip(t *testing.T) {
	requireShim(t)
	if err := avdevice.RegisterAll(); err != nil {
		t.Fatal(err)
	}
	src, err := NewDecoderWithOptions("testsrc2=size=160x90:rate=24:duration=1,format=yuv420p10le", &DecoderOptions{Format: "lavfi"})
	if err != nil {
		t.Fatalf("lavfi video source: %v", err)
	}
	defer src.Close()
	aud, err := NewDecoderWithOptions("sine=frequency=440:duration=1:sample_rate=48000,aformat=channel_layouts=5.1(side)", &DecoderOptions{Format: "lavfi"})
	if err != nil {
		t.Fatalf("lavfi audio source: %v", err)
	}
	defer aud.Close()

	path := filepath.Join(t.TempDir(), "needs.mkv")
	writeNeedsClip(t, path, src, aud)

	d, err := NewDecoder(path)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	checkNeedsClip(t, d)
}

// writeNeedsClip encodes src's video (HEVC Main 10, BT.2020/PQ, HDR10 side
// data on the encoder and the stream) and aud's audio (AC-3 5.1(side) with
// non-default mix levels, language jpn) into a Matroska file at path, with
// two chapters and two format tags.
func writeNeedsClip(t *testing.T, path string, src, aud *Decoder) {
	t.Helper()
	m, err := NewMuxer(path, "matroska")
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	if err := m.SetMetadata(Metadata{"title": "Clip", "CLUSTARR_PROFILE": "standard@abc"}); err != nil {
		t.Fatal(err)
	}
	if err := m.SetChapters([]Chapter{
		{ID: 1, Start: 0, End: 500 * time.Millisecond, Title: "One"},
		{ID: 2, Start: 500 * time.Millisecond, End: time.Second, Title: "Two"},
	}); err != nil {
		t.Fatal(err)
	}

	vs := src.VideoStream()
	vdec, err := src.NewStreamDecoder(vs.Index, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer vdec.Close()
	md, cll := masteringDisplayBlob(), lightLevelBlob()
	venc, err := NewVideoStreamEncoder(VideoStreamEncoderConfig{
		VideoEncoderConfig: VideoEncoderConfig{
			Codec: CodecIDHEVC, EncoderName: "libx265", Width: 160, Height: 90,
			FrameRate: NewRational(24, 1), PixelFormat: PixelFormatYUV420P10LE(),
			CodecOptions: map[string]string{
				"x265-params":     "log-level=error",
				"color_primaries": "bt2020", "color_trc": "smpte2084", "colorspace": "bt2020nc", "color_range": "tv",
			},
		},
		TimeBase:     vdec.TimeBase(),
		SideData:     map[FrameSideDataType][]byte{FrameSideMasteringDisplay(): md, FrameSideContentLightLevel(): cll},
		GlobalHeader: m.NeedsGlobalHeader(),
	})
	if errors.Is(err, ErrNotSupported) {
		t.Skipf("encoder side data needs FFmpeg 7: %v", err)
	}
	if err != nil {
		t.Fatal(err)
	}
	defer venc.Close()
	vms, err := m.AddEncoderStream(venc, StreamOptions{
		SideData: map[PacketSideDataType][]byte{PacketSideMasteringDisplay(): md, PacketSideContentLightLevel(): cll},
	})
	if err != nil {
		t.Fatal(err)
	}

	as := aud.AudioStream()
	adec, err := aud.NewStreamDecoder(as.Index, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer adec.Close()
	aenc, err := NewAudioEncoder(AudioEncoderConfig2{
		EncoderName: "ac3", SampleRate: 48000, Layout: "5.1(side)", BitRate: 384000,
		GlobalHeader: m.NeedsGlobalHeader(),
		Options:      map[string]string{"center_mixlev": "0.5", "surround_mixlev": "0.5"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer aenc.Close()
	ams, err := m.AddEncoderStream(aenc, StreamOptions{Language: "jpn"})
	if err != nil {
		t.Fatal(err)
	}
	if err := m.WriteHeader(); err != nil {
		t.Fatal(err)
	}

	emitV := func(p *Packet) error { return m.WritePacket(vms, p) }
	frames := 0
	decodeStream(t, src, vdec, func(f Frame) {
		f.SetColorSpec(ColorSpec{Range: ColorRangeMPEG, Space: 9, Primaries: 9, Transfer: 16}) // BT.2020 NCL, PQ
		if err := venc.Encode(f, emitV); err != nil {
			t.Fatal(err)
		}
		frames++
	})
	if err := venc.Flush(emitV); err != nil {
		t.Fatal(err)
	}
	if frames != 24 {
		t.Fatalf("encoded %d video frames from lavfi, want 24", frames)
	}

	// lavfi hands out packed samples; the AC-3 encoder takes planar float.
	emitA := func(p *Packet) error { return m.WritePacket(ams, p) }
	var g *FilterGraph
	encode := func(out []*Frame) {
		for _, o := range out {
			if err := aenc.Encode(*o, emitA); err != nil {
				t.Fatal(err)
			}
			_ = o.Free()
		}
	}
	decodeStream(t, aud, adec, func(f Frame) {
		if g == nil {
			if g, err = NewFilterGraph(FilterGraphConfig{
				SampleRate: 48000, SampleFmt: SampleFormat(f.Format()), Layout: f.ChannelLayout(),
				TimeBase: NewRational(1, 48000), Filters: "aformat=sample_fmts=fltp",
			}); err != nil {
				t.Fatal(err)
			}
		}
		out, err := g.Filter(&f)
		if err != nil {
			t.Fatal(err)
		}
		encode(out)
	})
	if g == nil {
		t.Fatal("no audio from lavfi")
	}
	out, err := g.Flush()
	if err != nil {
		t.Fatal(err)
	}
	encode(out)
	_ = g.Close()
	if err := aenc.Flush(emitA); err != nil {
		t.Fatal(err)
	}
	if err := m.WriteTrailer(); err != nil {
		t.Fatal(err)
	}
}

// checkNeedsClip reads writeNeedsClip's file back through ffgo.
func checkNeedsClip(t *testing.T, d *Decoder) {
	t.Helper()
	meta := d.GetMetadata()
	if meta["title"] != "Clip" || meta["CLUSTARR_PROFILE"] != "standard@abc" {
		t.Errorf("format tags = %v, want title=Clip and CLUSTARR_PROFILE=standard@abc", meta)
	}
	ch := d.GetChapters()
	if len(ch) != 2 || ch[0].Start != 0 || ch[0].Title != "One" || ch[1].Start != 500*time.Millisecond || ch[1].Title != "Two" {
		t.Errorf("chapters = %+v, want One at 0 and Two at 500ms", ch)
	}
	streams := d.Streams()
	if len(streams) != 2 {
		t.Fatalf("%d streams, want 2", len(streams))
	}
	v := streams[0]
	if v.Codec != "hevc" || ProfileName(v.CodecID, v.Profile) != "Main 10" {
		t.Errorf("stream 0 = %s profile %q, want hevc Main 10", v.Codec, ProfileName(v.CodecID, v.Profile))
	}
	if p, tr, sp, rg := v.Color.Names(); p != "bt2020" || tr != "smpte2084" || sp != "bt2020nc" || rg != "tv" {
		t.Errorf("stream 0 colour = %s/%s/%s/%s, want bt2020/smpte2084/bt2020nc/tv", p, tr, sp, rg)
	}
	md, ok, err := StreamSideData(v.CodecParameters(), PacketSideMasteringDisplay())
	if err != nil || !ok || len(md) < 88 {
		t.Fatalf("stream 0 mastering display: %d bytes, ok %v, %v", len(md), ok, err)
	}
	q := func(i int) float64 {
		num := int32(binary.LittleEndian.Uint32(md[8*i:]))
		den := int32(binary.LittleEndian.Uint32(md[8*i+4:]))
		if den == 0 {
			return math.NaN()
		}
		return float64(num) / float64(den)
	}
	for i := 0; i < 4; i++ {
		for j := 0; j < 2; j++ {
			if got, want := q(2*i+j), float64(needsPrimaries[i][j])/50000; math.Abs(got-want) > 1e-6 {
				t.Errorf("mastering display chromaticity %d.%d = %v, want %v", i, j, got, want)
			}
		}
	}
	if got := q(8); math.Abs(got-0.005) > 1e-9 {
		t.Errorf("min luminance = %v, want 0.005", got)
	}
	if got := q(9); math.Abs(got-1000) > 1e-6 {
		t.Errorf("max luminance = %v, want 1000", got)
	}
	if hasP, hasL := binary.LittleEndian.Uint32(md[80:]), binary.LittleEndian.Uint32(md[84:]); hasP != 1 || hasL != 1 {
		t.Errorf("has_primaries, has_luminance = %d, %d; want 1, 1", hasP, hasL)
	}
	cll, ok, err := StreamSideData(v.CodecParameters(), PacketSideContentLightLevel())
	if err != nil || !ok || len(cll) != 8 {
		t.Fatalf("stream 0 light level: %d bytes, ok %v, %v", len(cll), ok, err)
	}
	if c, f := binary.LittleEndian.Uint32(cll), binary.LittleEndian.Uint32(cll[4:]); c != needsMaxCLL || f != needsMaxFALL {
		t.Errorf("MaxCLL, MaxFALL = %d, %d; want %d, %d", c, f, needsMaxCLL, needsMaxFALL)
	}
	a := streams[1]
	if a.Codec != "ac3" || a.ChannelLayout != "5.1(side)" || a.Language != "jpn" {
		t.Errorf("stream 1 = %s %s %q, want ac3 5.1(side) jpn", a.Codec, a.ChannelLayout, a.Language)
	}
}

// AC-3's center_mixlev and surround_mixlev are written into every frame's
// bit stream information, so the same samples encode differently with
// them; an option the encoder does not have is an error.
func TestAudioEncoderOptionsReachTheEncoder(t *testing.T) {
	requireShim(t)
	firstPacket := func(opts map[string]string) []byte {
		t.Helper()
		enc, err := NewAudioEncoder(AudioEncoderConfig2{
			EncoderName: "ac3", SampleRate: 48000, Layout: "5.1(side)", BitRate: 384000, Options: opts,
		})
		if err != nil {
			t.Fatal(err)
		}
		defer enc.Close()
		f, err := NewAudioFrame(SampleFormatFLTP, 48000, "5.1(side)", 2*enc.FrameSize())
		if err != nil {
			t.Fatal(err)
		}
		defer f.Free()
		f.SetPTS(0)
		var first []byte
		emit := func(p *Packet) error {
			if first == nil {
				first = p.Data()
			}
			return nil
		}
		if err := enc.Encode(f, emit); err != nil {
			t.Fatal(err)
		}
		if err := enc.Flush(emit); err != nil {
			t.Fatal(err)
		}
		if len(first) == 0 {
			t.Fatal("no packet")
		}
		return first
	}
	if bytes.Equal(firstPacket(nil), firstPacket(map[string]string{"center_mixlev": "0.5", "surround_mixlev": "0.5"})) {
		t.Error("the mix level options left the AC-3 bit stream unchanged")
	}
	_, err := NewAudioEncoder(AudioEncoderConfig2{
		EncoderName: "ac3", SampleRate: 48000, Layout: "5.1(side)", BitRate: 384000,
		Options: map[string]string{"no_such_option": "1"},
	})
	if err == nil || !strings.Contains(err.Error(), "no_such_option") {
		t.Errorf("err = %v, want an error naming no_such_option", err)
	}
	if errors.Is(err, ErrShimRequired) {
		t.Errorf("err = %v: an unknown option is not a missing shim", err)
	}
}
