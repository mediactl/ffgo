//go:build !ios && !android && (amd64 || arm64)

package ffgo

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

const testSRT = "1\n00:00:01,000 --> 00:00:02,500\n<i>Hello</i> world\n\n2\n00:00:03,000 --> 00:00:04,000\nSecond line\n"

// subtitleClip is a 5 s MKV with an H.264 stream and testSRT's two cues as a
// subtitle stream encoded with codec ("srt" or "ass").
func subtitleClip(t *testing.T, codec string) string {
	t.Helper()
	ffmpegOrSkip(t)
	dir := t.TempDir()
	srt := filepath.Join(dir, "in.srt")
	if err := os.WriteFile(srt, []byte(testSRT), 0o644); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "sub-"+codec+".mkv")
	run(t, "ffmpeg", "-hide_banner", "-loglevel", "error", "-y",
		"-f", "lavfi", "-i", "color=c=black:s=64x36:r=5:d=5", "-i", srt,
		"-map", "0", "-map", "1", "-c:v", "libx264", "-c:s", codec, path)
	return path
}

func TestSubtitleDecoderFillsPTSAndEndTime(t *testing.T) {
	requireShim(t)
	d, err := NewDecoder(subtitleClip(t, "srt"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	ss := d.SubtitleStream()
	sub, err := NewSubtitleDecoder(ss)
	if err != nil {
		t.Fatal(err)
	}
	defer sub.Close()
	var got []*Subtitle
	for {
		p, err := d.ReadPacket()
		if err != nil {
			t.Fatal(err)
		}
		if p == nil {
			break
		}
		if p.StreamIndex() != ss.Index {
			continue
		}
		s, err := sub.Decode(p)
		if err != nil {
			t.Fatal(err)
		}
		if s != nil {
			got = append(got, s)
		}
	}
	if len(got) != 2 {
		t.Fatalf("decoded %d cues, want 2", len(got))
	}
	for i, w := range []struct {
		pts int64
		end time.Duration
	}{{1_000_000, 1500 * time.Millisecond}, {3_000_000, time.Second}} {
		if got[i].PTS != w.pts || got[i].EndTime != w.end {
			t.Errorf("cue %d: PTS %d EndTime %v, want %d %v", i+1, got[i].PTS, got[i].EndTime, w.pts, w.end)
		}
	}
}
