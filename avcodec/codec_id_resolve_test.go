//go:build !ios && !android && (amd64 || arm64)

package avcodec

import (
	"strings"
	"testing"

	"github.com/obinnaokechukwu/ffgo/internal/bindings"
)

func TestFindEncoderByConstantFindsThatCodecOnTheLoadedRelease(t *testing.T) {
	if err := bindings.Load(); err != nil {
		t.Skip(err)
	}
	// AV_CODEC_ID_HEVC is 173 through FFmpeg 7 and 172 in FFmpeg 9; the
	// constant must still find an HEVC encoder.
	if FindEncoderByName("libx265") == nil {
		t.Skip("no HEVC encoder in this FFmpeg")
	}
	c := FindEncoder(CodecIDHEVC)
	if c == nil {
		t.Fatal("FindEncoder(CodecIDHEVC) found nothing, though libx265 is here")
	}
	if name := GetCodecName(c); !strings.Contains(name, "265") && !strings.Contains(name, "hevc") {
		t.Errorf("FindEncoder(CodecIDHEVC) found %q", name)
	}
}

func TestEveryNamedCodecConstantResolvesOnTheLoadedRelease(t *testing.T) {
	if err := bindings.Load(); err != nil {
		t.Skip(err)
	}
	for id, name := range codecNames {
		got := Resolve(id)
		if want := CodecIDByName(name); got != want || want == CodecIDNone {
			t.Errorf("%s: Resolve(%d) = %d, CodecIDByName = %d", name, id, got, want)
		}
		if got != id {
			t.Logf("%s: ffgo constant %d is %d in the loaded release", name, id, got)
		}
	}
}
