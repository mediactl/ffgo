//go:build !ios && !android && (amd64 || arm64)

package ffgo

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

// everythingClip is an MKV with H.264 video; English AC-3 5.1 (default);
// French FLAC stereo titled "Commentary" (comment); English SRT (forced); a
// font attachment; two chapters; and the container title "Clip".
func everythingClip(t *testing.T) string {
	t.Helper()
	ffmpegOrSkip(t)
	dir := t.TempDir()
	srt := filepath.Join(dir, "sub.srt")
	font := filepath.Join(dir, "font.ttf")
	meta := filepath.Join(dir, "chapters.ffmeta")
	for path, body := range map[string]string{
		srt:  "1\n00:00:00,500 --> 00:00:01,500\nHello\n\n2\n00:00:02,000 --> 00:00:02,800\nWorld\n",
		font: "not really a font, but the bytes are what is checked",
		meta: ";FFMETADATA1\n[CHAPTER]\nTIMEBASE=1/1000\nSTART=0\nEND=1500\ntitle=One\n[CHAPTER]\nTIMEBASE=1/1000\nSTART=1500\nEND=3000\ntitle=Two\n",
	} {
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	out := filepath.Join(dir, "everything.mkv")
	run(t, "ffmpeg", "-hide_banner", "-loglevel", "error", "-y",
		"-f", "lavfi", "-i", "testsrc2=size=160x90:rate=25:duration=3",
		"-f", "lavfi", "-i", "sine=frequency=440:sample_rate=48000:duration=3,aformat=channel_layouts=5.1",
		"-f", "lavfi", "-i", "sine=frequency=880:sample_rate=48000:duration=3,aformat=channel_layouts=stereo",
		"-i", srt, "-i", meta,
		"-map", "0", "-map", "1", "-map", "2", "-map", "3", "-map_chapters", "4",
		"-c:v", "libx264", "-c:a:0", "ac3", "-c:a:1", "flac", "-c:s", "srt",
		"-attach", font, "-metadata:s:t", "mimetype=application/x-truetype-font",
		"-metadata:s:a:0", "language=eng", "-metadata:s:a:1", "language=fre", "-metadata:s:a:1", "title=Commentary",
		"-metadata:s:s:0", "language=eng",
		"-disposition:a:0", "default", "-disposition:a:1", "comment", "-disposition:s:0", "forced",
		"-metadata", "title=Clip", out)
	return out
}

type probed struct {
	Streams []struct {
		CodecName   string            `json:"codec_name"`
		CodecType   string            `json:"codec_type"`
		Channels    int               `json:"channels"`
		Tags        map[string]string `json:"tags"`
		Disposition map[string]int    `json:"disposition"`
	} `json:"streams"`
	Chapters []struct {
		Tags map[string]string `json:"tags"`
	} `json:"chapters"`
	Format struct {
		Duration string            `json:"duration"`
		Tags     map[string]string `json:"tags"`
	} `json:"format"`
}

func ffprobeJSON(t *testing.T, path string) probed {
	t.Helper()
	var p probed
	out := run(t, "ffprobe", "-v", "error", "-show_streams", "-show_chapters", "-show_format", "-of", "json", path)
	if err := json.Unmarshal([]byte(out), &p); err != nil {
		t.Fatalf("ffprobe json: %v\n%s", err, out)
	}
	return p
}

func (p probed) duration(t *testing.T) float64 {
	t.Helper()
	d, err := strconv.ParseFloat(p.Format.Duration, 64)
	if err != nil {
		t.Fatalf("duration %q: %v", p.Format.Duration, err)
	}
	return d
}

func TestMuxerRebuildsAFileStreamByStream(t *testing.T) {
	requireShim(t)
	src := everythingClip(t)
	out := filepath.Join(t.TempDir(), "out.mkv")

	d, err := NewDecoder(src)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	m, err := NewMuxer(out, "matroska")
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()

	streams := map[int]*MuxerStream{}
	var (
		flac *StreamInfo
		dec  *StreamDecoder
		enc  *AudioEncoder
		res  *Resampler
	)
	for _, s := range d.Streams() {
		if s.Type == MediaTypeAttachment {
			continue // attachments are written by AddAttachment
		}
		opts := StreamOptions{Language: s.Language, Title: s.Title, Disposition: s.Disposition, Metadata: s.Metadata}
		if s.Type == MediaTypeAudio && s.CodecName == "flac" {
			flac = s
			if dec, err = d.NewStreamDecoder(s.Index, nil); err != nil {
				t.Fatal(err)
			}
			defer dec.Close()
			if enc, err = NewAudioEncoder(AudioEncoderConfig2{SampleRate: 48000, Layout: "stereo", BitRate: 128000,
				GlobalHeader: m.NeedsGlobalHeader(), InputTimeBase: dec.TimeBase()}); err != nil {
				t.Fatal(err)
			}
			defer enc.Close()
			streams[s.Index], err = m.AddEncoderStream(enc, opts)
		} else {
			streams[s.Index], err = m.AddCopyStream(&CopyStreamConfig{CodecParameters: s.CodecParameters(), TimeBase: s.TimeBase, Options: opts})
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, a := range d.GetAttachments() {
		if err := m.AddAttachment(a); err != nil {
			t.Fatal(err)
		}
	}
	if err := m.SetChapters(d.GetChapters()); err != nil {
		t.Fatal(err)
	}
	if err := m.SetMetadata(d.GetMetadata()); err != nil {
		t.Fatal(err)
	}
	if err := m.WriteHeader(); err != nil {
		t.Fatal(err)
	}

	ms := streams[flac.Index]
	emit := func(p *Packet) error { return m.WritePacket(ms, p) }
	encode := func(f Frame) {
		if res == nil {
			if res, err = NewResampler(AudioFormat{SampleRate: flac.SampleRate, Layout: flac.ChannelLayout, SampleFormat: SampleFormat(f.Format())},
				AudioFormat{SampleRate: 48000, Layout: "stereo", SampleFormat: SampleFormatFLTP}); err != nil {
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
	drain := func() {
		for {
			f, err := dec.Receive()
			if errors.Is(err, ErrAgain) || errors.Is(err, io.EOF) {
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			encode(f)
		}
	}
	for {
		p, err := d.ReadPacket()
		if err != nil {
			t.Fatal(err)
		}
		if p == nil {
			break
		}
		idx := p.StreamIndex()
		if idx == flac.Index {
			if err := dec.Send(p); err != nil {
				t.Fatal(err)
			}
			drain()
			continue
		}
		if out, ok := streams[idx]; ok {
			c, err := p.Clone()
			if err != nil {
				t.Fatal(err)
			}
			if err := m.WritePacket(out, c); err != nil {
				t.Fatal(err)
			}
			_ = c.Free()
		}
	}
	if err := dec.Send(nil); err != nil {
		t.Fatal(err)
	}
	drain()
	if res != nil {
		defer res.Close()
	}
	if err := enc.Flush(emit); err != nil {
		t.Fatal(err)
	}
	if err := m.WriteTrailer(); err != nil {
		t.Fatal(err)
	}

	p := ffprobeJSON(t, out)
	if len(p.Streams) != 5 {
		t.Fatalf("%d streams, want 5", len(p.Streams))
	}
	check := func(i int, field, got, want string) {
		if got != want {
			t.Errorf("stream %d %s = %q, want %q", i, field, got, want)
		}
	}
	check(0, "codec", p.Streams[0].CodecName, "h264")
	check(1, "codec", p.Streams[1].CodecName, "ac3")
	check(1, "language", p.Streams[1].Tags["language"], "eng")
	check(1, "default", strconv.Itoa(p.Streams[1].Disposition["default"]), "1")
	check(2, "codec", p.Streams[2].CodecName, "aac")
	check(2, "language", p.Streams[2].Tags["language"], "fre")
	check(2, "title", p.Streams[2].Tags["title"], "Commentary")
	check(2, "comment", strconv.Itoa(p.Streams[2].Disposition["comment"]), "1")
	check(2, "default", strconv.Itoa(p.Streams[2].Disposition["default"]), "0")
	check(3, "codec", p.Streams[3].CodecName, "subrip")
	check(3, "forced", strconv.Itoa(p.Streams[3].Disposition["forced"]), "1")
	check(4, "type", p.Streams[4].CodecType, "attachment")
	check(4, "filename", p.Streams[4].Tags["filename"], "font.ttf")
	if len(p.Chapters) != 2 || p.Chapters[0].Tags["title"] != "One" || p.Chapters[1].Tags["title"] != "Two" {
		t.Errorf("chapters %+v, want One and Two", p.Chapters)
	}
	check(-1, "title", p.Format.Tags["title"], "Clip")
	if got, want := p.duration(t), ffprobeJSON(t, src).duration(t); got < want-0.1 || got > want+0.1 {
		t.Errorf("duration %.3f s, source %.3f s", got, want)
	}
}
