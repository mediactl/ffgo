//go:build !ios && !android && (amd64 || arm64)

package ffgo

import (
	"strconv"
	"strings"
	"testing"

	"github.com/obinnaokechukwu/ffgo/avformat"
	"github.com/obinnaokechukwu/ffgo/internal/shim"
)

// The demuxer stops reading the other streams (AVStream.discard), and
// ReadPacket drops the packets it had buffered before Discard
// (avformat_find_stream_info reads ahead), as ffmpeg(1) never passes them on.
func TestDiscardKeepsOnlyTheNamedStreamsPackets(t *testing.T) {
	requireShim(t)
	path := gopClip(t, "mkv")
	want := strings.TrimSpace(run(t, "ffprobe", "-v", "error", "-select_streams", "a:0", "-count_packets",
		"-show_entries", "stream=nb_read_packets", "-of", "csv=p=0", path))
	d, err := NewDecoder(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close() }()
	audio := d.AudioStream().Index
	if err := d.Discard(audio); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < d.NumStreams(); i++ {
		want := avformat.AVDiscardAll
		if i == audio {
			want = 0 // AVDISCARD_DEFAULT
		}
		if got := avformat.GetStreamDiscard(avformat.GetStream(d.formatCtx, i)); got != want {
			t.Errorf("stream %d: AVStream.discard = %d, want %d", i, got, want)
		}
	}
	n := 0
	for {
		p, err := d.ReadPacket()
		if err != nil {
			t.Fatal(err)
		}
		if p == nil {
			break
		}
		if p.StreamIndex() != audio {
			t.Fatalf("read a packet of stream %d after Discard(%d)", p.StreamIndex(), audio)
		}
		n++
	}
	if strconv.Itoa(n) != want {
		t.Fatalf("read %d audio packets, ffprobe counts %s", n, want)
	}
}

func TestDiscardRefusesAStreamTheInputLacks(t *testing.T) {
	requireShim(t)
	d, err := NewDecoder(gopClip(t, "mkv"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close() }()
	if err := d.Discard(99); err == nil {
		t.Fatal("Discard(99) on a two-stream input succeeded")
	}
}

func TestAVDiscardAllMatchesTheHeaders(t *testing.T) {
	requireShim(t)
	if v, ok := shim.EnumValue("AVDISCARD_ALL"); !ok || int32(v) != avformat.AVDiscardAll {
		t.Fatalf("AVDISCARD_ALL: headers %d (known %v), ffgo %d", v, ok, avformat.AVDiscardAll)
	}
}
