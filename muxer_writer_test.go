//go:build !ios && !android && (amd64 || arm64)

package ffgo

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

// A writer-backed mp4 muxer with frag_custom writes one moof per Flush, and
// what it wrote is an MP4 ffprobe decodes whole. This is what cluster-plex's
// remux worker cuts DASH segments from.
func TestAWriterMuxerEmitsOneFragmentPerFlush(t *testing.T) {
	clip := h264Clip(t, 72) // 3 s at 24 fps
	d, err := NewDecoder(clip)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	var src *StreamInfo
	for _, s := range d.Streams() {
		if s.Type == MediaTypeVideo {
			src = s
		}
	}
	var out bytes.Buffer
	m, err := NewMuxerToWriter(&out, "mp4")
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	ms, err := m.AddCopyStream(&CopyStreamConfig{CodecParameters: src.CodecParameters(), TimeBase: src.TimeBase})
	if err != nil {
		t.Fatal(err)
	}
	if err := m.WriteHeaderWithOptions(map[string]string{"movflags": "frag_custom+empty_moov+default_base_moof"}); err != nil {
		t.Fatal(err)
	}
	if tb := ms.OutputTimeBase(); tb.Num == 0 || tb.Den == 0 {
		t.Fatalf("output time base after the header = %v, want the muxer's", tb)
	}
	if err := m.Flush(); err != nil { // the init reaches the writer
		t.Fatal(err)
	}
	if got := countBoxes(out.Bytes(), "moov"); got != 1 {
		t.Fatalf("after the header and a Flush: %d moov, want 1", got)
	}
	flushes, n := 0, 0
	for {
		p, err := d.ReadPacket()
		if err != nil {
			t.Fatal(err)
		}
		if p == nil {
			break
		}
		if p.StreamIndex() != src.Index {
			continue
		}
		if n > 0 && n%24 == 0 {
			if err := m.Flush(); err != nil {
				t.Fatal(err)
			}
			flushes++
		}
		n++
		c, err := p.Clone()
		if err != nil {
			t.Fatal(err)
		}
		if err := m.WritePacket(ms, c); err != nil {
			t.Fatal(err)
		}
		_ = c.Free()
	}
	if err := m.Flush(); err != nil {
		t.Fatal(err)
	}
	flushes++
	if err := m.WriteTrailer(); err != nil {
		t.Fatal(err)
	}
	if got := countBoxes(out.Bytes(), "moof"); got != flushes {
		t.Fatalf("moof boxes = %d, want one per Flush (%d)", got, flushes)
	}
	path := filepath.Join(t.TempDir(), "out.mp4")
	if err := os.WriteFile(path, out.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := ffprobeFrameCount(t, path, 0); got != 72 {
		t.Fatalf("frames decoded = %d, want 72", got)
	}
}

// countBoxes counts the top-level ISO BMFF boxes of type typ in b.
func countBoxes(b []byte, typ string) int {
	n := 0
	for len(b) >= 8 {
		size, hdr := int(binary.BigEndian.Uint32(b)), 8
		if size == 1 && len(b) >= 16 {
			size, hdr = int(binary.BigEndian.Uint64(b[8:])), 16
		}
		if size < hdr || size > len(b) {
			break
		}
		if string(b[4:8]) == typ {
			n++
		}
		b = b[size:]
	}
	return n
}
