//go:build !ios && !android && (amd64 || arm64)

package ffgo

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Matroska stores chapter times in nanoseconds (time base 1/1000000000), so
// ts*num*1000000 overflows int64 past 9.22e12 ns: 2h33m43s.
func TestChapterTimesPastTheInt64MicrosecondRange(t *testing.T) {
	requireShim(t)
	ffmpegOrSkip(t)
	dir := t.TempDir()
	meta := filepath.Join(dir, "chapters.txt")
	if err := os.WriteFile(meta, []byte(";FFMETADATA1\n"+
		"[CHAPTER]\nTIMEBASE=1/1000000000\nSTART=0\nEND=600000000\ntitle=Opening\n"+
		"[CHAPTER]\nTIMEBASE=1/1000000000\nSTART=600000000\nEND=10800000000000\ntitle=Long\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "long.mkv")
	run(t, "ffmpeg", "-hide_banner", "-loglevel", "error", "-y",
		"-f", "lavfi", "-i", "testsrc2=size=64x36:rate=5:duration=1", "-i", meta,
		"-map", "0", "-map_chapters", "1", "-c:v", "libx264", path)
	d, err := NewDecoder(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close() }()
	ch := d.GetChapters()
	if len(ch) != 2 {
		t.Fatalf("got %d chapters, want 2", len(ch))
	}
	c := ch[1]
	if c.Title != "Long" || c.Start != 600*time.Millisecond || c.End != 3*time.Hour {
		t.Errorf("chapter 2 = %q %v-%v, want Long 600ms-3h0m0s", c.Title, c.Start, c.End)
	}
	if c.TimeBase != NewRational(1, 1000000000) || c.StartTS != 600000000 || c.EndTS != 10800000000000 {
		t.Errorf("chapter 2 timestamps = %d-%d in %d/%d, want 600000000-10800000000000 in 1/1000000000",
			c.StartTS, c.EndTS, c.TimeBase.Num, c.TimeBase.Den)
	}
}
