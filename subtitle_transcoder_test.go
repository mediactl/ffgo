//go:build !ios && !android && (amd64 || arm64)

package ffgo

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// srtFrame frames events as libavformat/srtenc.c writes them, with packet
// times in milliseconds (the srt muxer's time base): what
// `ffmpeg -c:s srt -f srt` writes.
func srtFrame(evs []SubtitleEvent) []byte {
	var b bytes.Buffer
	us, ms := NewRational(1, 1000000), NewRational(1, 1000)
	n := 1
	for _, ev := range evs {
		s, d := RescaleQ(ev.PTS, us, ms), RescaleQ(ev.Duration, us, ms)
		if d < 0 {
			continue
		}
		e := s + d
		fmt.Fprintf(&b, "%d\n%02d:%02d:%02d,%03d --> %02d:%02d:%02d,%03d\n", n,
			s/3600000, s/60000%60, s/1000%60, s%1000, e/3600000, e/60000%60, e/1000%60, e%1000)
		b.Write(ev.Data)
		b.WriteString("\n\n")
		n++
	}
	return b.Bytes()
}

func TestSubtitleTranscoderMatchesFFmpegsSRT(t *testing.T) {
	requireShim(t)
	for _, codec := range []string{"srt", "ass"} {
		t.Run(codec, func(t *testing.T) {
			path := subtitleClip(t, codec)
			ref := filepath.Join(t.TempDir(), "ref.srt")
			run(t, "ffmpeg", "-hide_banner", "-loglevel", "error", "-y", "-i", path, "-map", "0:s:0", "-c:s", "srt", "-f", "srt", ref)
			want, err := os.ReadFile(ref)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Contains(want, []byte("<i>Hello</i>")) {
				t.Fatalf("fixture lost its italics: %q", want)
			}
			d, err := NewDecoder(path)
			if err != nil {
				t.Fatal(err)
			}
			defer d.Close()
			idx := d.SubtitleStreamIndex()
			tr, err := d.NewSubtitleTranscoder(idx, "srt")
			if err != nil {
				t.Fatal(err)
			}
			defer tr.Close()
			var evs []SubtitleEvent
			add := func(ev SubtitleEvent, ok bool, err error) bool {
				if err != nil {
					t.Fatal(err)
				}
				if ok {
					evs = append(evs, ev)
				}
				return ok
			}
			for {
				p, err := d.ReadPacket()
				if err != nil {
					t.Fatal(err)
				}
				if p == nil {
					break
				}
				if p.StreamIndex() == idx {
					add(tr.Transcode(p))
				}
			}
			for add(tr.Transcode(nil)) {
			}
			if got := srtFrame(evs); !bytes.Equal(got, want) {
				t.Fatalf("ffgo:\n%s\nffmpeg:\n%s", got, want)
			}
		})
	}
}

func TestSubtitleTranscoderRefusesANonSubtitleStream(t *testing.T) {
	requireShim(t)
	d, err := NewDecoder(subtitleClip(t, "srt"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if _, err := d.NewSubtitleTranscoder(0, "srt"); err == nil { // stream 0 is the video
		t.Fatal("NewSubtitleTranscoder on a video stream succeeded")
	}
}
