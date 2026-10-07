//go:build !ios && !android && (amd64 || arm64)

package ffgo

import (
	"bytes"
	"path/filepath"
	"testing"
)

// A Muxer writes pre-made SubRip and ASS events with the stream's language
// and disposition, and ffgo's own demuxer and subtitle transcoder read them
// back unchanged: what internal/testmedia needs instead of `ffmpeg -i x.srt`.
func TestMuxerWritesTextSubtitleEvents(t *testing.T) {
	requireShim(t)
	for _, codec := range []string{"subrip", "ass"} {
		t.Run(codec, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "subs.mkv")
			m, err := NewMuxer(path, "matroska")
			if err != nil {
				t.Fatal(err)
			}
			ms, err := m.AddSubtitleStream(&SubtitleStreamConfig{Codec: codec,
				Options: StreamOptions{Language: "eng", Disposition: DispositionForced}})
			if err != nil {
				t.Fatal(err)
			}
			if err := m.WriteHeader(); err != nil {
				t.Fatal(err)
			}
			text := "Hello"
			if codec == "ass" {
				text = "0,0,Default,,0,0,0,,Hello"
			}
			for i, at := range []int64{500_000, 2_000_000} {
				if err := m.WriteSubtitle(ms, SubtitleEvent{PTS: at, Duration: 1_000_000, Data: []byte(text)}); err != nil {
					t.Fatalf("event %d: %v", i, err)
				}
			}
			if err := m.WriteTrailer(); err != nil {
				t.Fatal(err)
			}
			if err := m.Close(); err != nil {
				t.Fatal(err)
			}

			d, err := NewDecoder(path)
			if err != nil {
				t.Fatal(err)
			}
			defer d.Close()
			info := d.Streams()
			if len(info) != 1 || info[0].Type != MediaTypeSubtitle || info[0].Language != "eng" ||
				info[0].Disposition&DispositionForced == 0 {
				t.Fatalf("streams = %+v, want one forced eng subtitle stream", info)
			}
			if want := map[string]string{"subrip": "subrip", "ass": "ass"}[codec]; info[0].Codec != want {
				t.Fatalf("codec = %q, want %q", info[0].Codec, want)
			}
			tr, err := d.NewSubtitleTranscoder(0, "srt")
			if err != nil {
				t.Fatal(err)
			}
			defer tr.Close()
			var got []SubtitleEvent
			add := func(ev SubtitleEvent, ok bool, err error) bool {
				if err != nil {
					t.Fatal(err)
				}
				if ok {
					got = append(got, ev)
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
				add(tr.Transcode(p))
			}
			for add(tr.Transcode(nil)) {
			}
			if len(got) != 2 || got[0].PTS != 500_000 || got[1].PTS != 2_000_000 || got[0].Duration != 1_000_000 ||
				!bytes.Contains(got[0].Data, []byte("Hello")) {
				t.Fatalf("events = %+v", got)
			}
		})
	}
}
