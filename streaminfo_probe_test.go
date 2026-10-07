//go:build !ios && !android && (amd64 || arm64)

package ffgo

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/obinnaokechukwu/ffgo/avutil"
	"github.com/obinnaokechukwu/ffgo/internal/shim"
)

// ffprobeStream is one entry of `ffprobe -show_streams -of json`.
type ffprobeStream map[string]any

// str is ffprobe's value for key as text, and whether ffprobe printed it:
// JSON output leaves out the fields ffprobe prints as "unknown" or N/A.
func (s ffprobeStream) str(key string) (string, bool) {
	v, ok := s[key]
	if !ok {
		return "", false
	}
	switch v := v.(type) {
	case string:
		return v, true
	case float64:
		return strconv.FormatInt(int64(v), 10), true
	default:
		return fmt.Sprint(v), true
	}
}

func ffprobeStreams(t *testing.T, path string) []ffprobeStream {
	t.Helper()
	var doc struct {
		Streams []ffprobeStream `json:"streams"`
	}
	if err := json.Unmarshal([]byte(run(t, "ffprobe", "-v", "error", "-show_streams", "-of", "json", path)), &doc); err != nil {
		t.Fatalf("ffprobe %s: %v", path, err)
	}
	return doc.Streams
}

// probeFixtures makes the clips TestStreamInfoReadsWhatFFprobePrints reads:
// HEVC Main 10 tagged BT.2020/PQ at 24000/1001 with 24-bit FLAC 5.1(side)
// and 16-bit PCM in three channels of no order (hdr.mkv); interlaced
// MPEG-2 with AC-3 (ilace.mkv); H.264 High with B-frames and AAC LC in MP4,
// whose streams carry their own start and duration (avc.mp4).
func probeFixtures(t *testing.T) map[string]string {
	t.Helper()
	ffmpegOrSkip(t)
	dir := t.TempDir()
	ff := func(name string, args ...string) {
		args = append([]string{"-hide_banner", "-loglevel", "error", "-y"}, args...)
		run(t, "ffmpeg", append(args, filepath.Join(dir, name))...)
	}
	ff("hdr.mkv",
		"-f", "lavfi", "-i", "testsrc2=size=128x72:rate=24000/1001:duration=1",
		"-f", "lavfi", "-i", "sine=frequency=440:duration=1:sample_rate=48000,aformat=channel_layouts=5.1(side)",
		"-f", "lavfi", "-i", "anullsrc=channel_layout=3c:sample_rate=48000",
		"-map", "0", "-map", "1", "-map", "2", "-t", "1",
		"-c:v", "libx265", "-pix_fmt", "yuv420p10le", "-x265-params", "log-level=error",
		"-color_primaries", "bt2020", "-color_trc", "smpte2084", "-colorspace", "bt2020nc", "-color_range", "tv",
		"-c:a:0", "flac", "-sample_fmt:a:0", "s32", "-bits_per_raw_sample:a:0", "24",
		"-c:a:1", "pcm_s16le")
	ff("ilace.mkv",
		"-f", "lavfi", "-i", "testsrc2=size=128x72:rate=25:duration=1",
		"-f", "lavfi", "-i", "sine=frequency=440:duration=1:sample_rate=48000,aformat=channel_layouts=5.1(side)",
		"-map", "0", "-map", "1", "-vf", "setfield=tff", "-flags:v", "+ilme+ildct",
		"-c:v", "mpeg2video", "-c:a", "ac3")
	ff("avc.mp4",
		"-f", "lavfi", "-i", "testsrc2=size=128x72:rate=24000/1001:duration=1",
		"-f", "lavfi", "-i", "sine=frequency=440:duration=1:sample_rate=48000",
		"-c:v", "libx264", "-profile:v", "high", "-bf", "3", "-c:a", "aac", "-t", "1")
	return map[string]string{
		"hdr.mkv":   filepath.Join(dir, "hdr.mkv"),
		"ilace.mkv": filepath.Join(dir, "ilace.mkv"),
		"avc.mp4":   filepath.Join(dir, "avc.mp4"),
	}
}

// streamInfoCheck compares one StreamInfo field with what ffprobe prints;
// got and want both "" means the field does not apply to the stream.
type streamInfoCheck struct {
	name  string
	check func(si *StreamInfo, ff ffprobeStream) (got, want string)
}

var streamInfoChecks = []streamInfoCheck{
	{"profile", func(si *StreamInfo, ff ffprobeStream) (string, string) {
		want, _ := ff.str("profile")
		got := ProfileName(si.CodecID, si.Profile)
		if got == "" && si.Profile != ProfileUnknown {
			got = strconv.Itoa(si.Profile) // ffprobe prints a nameless profile's number
		}
		return got, want
	}},
	{"level", func(si *StreamInfo, ff ffprobeStream) (string, string) {
		if si.Type != MediaTypeVideo {
			return "", ""
		}
		want, _ := ff.str("level")
		return strconv.Itoa(si.Level), want
	}},
	{"bits_per_raw_sample", func(si *StreamInfo, ff ffprobeStream) (string, string) {
		want, _ := ff.str("bits_per_raw_sample")
		if si.BitsPerRawSample == 0 {
			return "", want // ffprobe prints N/A, which JSON leaves out, for 0
		}
		return strconv.Itoa(si.BitsPerRawSample), want
	}},
	{"bits_per_sample", func(si *StreamInfo, ff ffprobeStream) (string, string) {
		if si.Type != MediaTypeAudio {
			return "", ""
		}
		want, _ := ff.str("bits_per_sample")
		return strconv.Itoa(BitsPerSample(si.CodecID)), want
	}},
	{"bits_per_coded_sample (pcm)", func(si *StreamInfo, _ ffprobeStream) (string, string) {
		if si.Codec != "pcm_s16le" {
			return "", ""
		}
		return strconv.Itoa(si.BitsPerCodedSample), "16"
	}},
	{"field_order", func(si *StreamInfo, ff ffprobeStream) (string, string) {
		if si.Type != MediaTypeVideo {
			return "", ""
		}
		want, _ := ff.str("field_order")
		return si.FieldOrder.String(), want
	}},
	{"color", func(si *StreamInfo, ff ffprobeStream) (string, string) {
		if si.Type != MediaTypeVideo {
			return "", ""
		}
		p, tr, sp, rg := si.Color.Names()
		var want []string
		for _, k := range []string{"color_primaries", "color_transfer", "color_space", "color_range"} {
			v, ok := ff.str(k)
			if !ok {
				v = "unknown" // ffprobe prints "unknown", which JSON leaves out
			}
			want = append(want, v)
		}
		return strings.Join([]string{p, tr, sp, rg}, ","), strings.Join(want, ",")
	}},
	{"r_frame_rate", func(si *StreamInfo, ff ffprobeStream) (string, string) {
		want, _ := ff.str("r_frame_rate")
		return fmt.Sprintf("%d/%d", si.RealFrameRate.Num, si.RealFrameRate.Den), want
	}},
	{"start_pts", func(si *StreamInfo, ff ffprobeStream) (string, string) {
		want, _ := ff.str("start_pts")
		return tsOrNA(si.StartTime), want
	}},
	{"duration_ts", func(si *StreamInfo, ff ffprobeStream) (string, string) {
		want, _ := ff.str("duration_ts")
		return tsOrNA(si.Duration), want
	}},
	{"channel_layout", func(si *StreamInfo, ff ffprobeStream) (string, string) {
		if si.Type != MediaTypeAudio {
			return "", ""
		}
		want, _ := ff.str("channel_layout")
		if si.ChannelOrder == ChannelOrderUnspec {
			return "", want
		}
		return si.ChannelLayout, want
	}},
}

// tsOrNA is a timestamp as ffprobe prints it: N/A (left out of JSON) for
// AV_NOPTS_VALUE.
func tsOrNA(ts int64) string {
	if ts == avutil.NoPTSValue {
		return ""
	}
	return strconv.FormatInt(ts, 10)
}

// fixtureFacts pin that the fixtures give each compared field a real value,
// since a comparison of two zeros passes whatever ffgo read. want "" means
// ffprobe must print the field, "-" that it must leave it out.
var fixtureFacts = []struct {
	file   string
	stream int
	key    string
	want   string
}{
	{"hdr.mkv", 0, "profile", "Main 10"},
	{"avc.mp4", 1, "profile", "LC"},
	{"avc.mp4", 0, "level", ""},
	{"hdr.mkv", 1, "bits_per_raw_sample", "24"},
	{"hdr.mkv", 2, "bits_per_sample", "16"},
	{"ilace.mkv", 0, "field_order", "tb"},
	{"hdr.mkv", 0, "field_order", "progressive"},
	{"hdr.mkv", 0, "color_space", "bt2020nc"},
	{"hdr.mkv", 0, "color_range", "tv"},
	{"hdr.mkv", 0, "r_frame_rate", "24000/1001"},
	{"avc.mp4", 0, "duration_ts", ""},
	{"hdr.mkv", 0, "duration_ts", "-"},
	{"hdr.mkv", 1, "channel_layout", "5.1(side)"},
	{"hdr.mkv", 2, "channel_layout", "-"},
}

func TestStreamInfoReadsWhatFFprobePrints(t *testing.T) {
	requireShim(t)
	paths := probeFixtures(t)
	probed := map[string][]ffprobeStream{}
	for name, path := range paths {
		probed[name] = ffprobeStreams(t, path)
	}
	for _, f := range fixtureFacts {
		got, ok := probed[f.file][f.stream].str(f.key)
		switch {
		case f.want == "-" && ok:
			t.Errorf("fixture %s stream %d: ffprobe prints %s=%q; the fixture must leave it unknown", f.file, f.stream, f.key, got)
		case f.want != "-" && !ok:
			t.Errorf("fixture %s stream %d: ffprobe prints no %s", f.file, f.stream, f.key)
		case f.want != "" && f.want != "-" && got != f.want:
			t.Errorf("fixture %s stream %d: ffprobe %s=%q, want %q", f.file, f.stream, f.key, got, f.want)
		}
	}
	for name, path := range paths {
		d, err := NewDecoder(path)
		if err != nil {
			t.Fatal(err)
		}
		streams := d.Streams()
		ff := probed[name]
		if len(streams) != len(ff) {
			t.Fatalf("%s: ffgo sees %d streams, ffprobe %d", name, len(streams), len(ff))
		}
		for i, si := range streams {
			for _, c := range streamInfoChecks {
				if got, want := c.check(si, ff[i]); got != want {
					t.Errorf("%s stream %d (%s) %s: ffgo %q, ffprobe %q", name, i, si.Codec, c.name, got, want)
				}
			}
		}
		_ = d.Close()
	}
}

func TestProfileUnknownMatchesTheHeaders(t *testing.T) {
	requireShim(t)
	if v, ok := shim.EnumValue("AV_PROFILE_UNKNOWN"); !ok || v != ProfileUnknown {
		t.Fatalf("AV_PROFILE_UNKNOWN: headers %d (known %v), ffgo %d", v, ok, ProfileUnknown)
	}
}

func TestFieldOrderMatchesTheHeaders(t *testing.T) {
	requireShim(t)
	for name, v := range map[string]FieldOrder{
		"AV_FIELD_UNKNOWN": FieldOrderUnknown, "AV_FIELD_PROGRESSIVE": FieldOrderProgressive,
		"AV_FIELD_TT": FieldOrderTT, "AV_FIELD_BB": FieldOrderBB, "AV_FIELD_TB": FieldOrderTB, "AV_FIELD_BT": FieldOrderBT,
	} {
		if got, ok := shim.EnumValue(name); !ok || FieldOrder(got) != v {
			t.Errorf("%s: headers %d (known %v), ffgo %d", name, got, ok, v)
		}
	}
}

func TestChannelOrderMatchesTheHeaders(t *testing.T) {
	requireShim(t)
	for name, v := range map[string]ChannelOrder{
		"AV_CHANNEL_ORDER_UNSPEC": ChannelOrderUnspec, "AV_CHANNEL_ORDER_NATIVE": ChannelOrderNative,
		"AV_CHANNEL_ORDER_CUSTOM": ChannelOrderCustom, "AV_CHANNEL_ORDER_AMBISONIC": ChannelOrderAmbisonic,
	} {
		if got, ok := shim.EnumValue(name); !ok || ChannelOrder(got) != v {
			t.Errorf("%s: headers %d (known %v), ffgo %d", name, got, ok, v)
		}
	}
}
