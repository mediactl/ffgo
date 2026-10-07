//go:build !ios && !android && (amd64 || arm64)

package ffgo

import (
	"testing"

	"github.com/obinnaokechukwu/ffgo/avutil"
)

func TestRescaleQRndRoundsAsFFmpegDoes(t *testing.T) {
	if err := Init(); err != nil {
		t.Skipf("FFmpeg libraries not available: %v", err)
	}
	half, one := NewRational(1, 2), NewRational(1, 1)
	for _, tc := range []struct {
		a    int64
		rnd  Rounding
		want int64
	}{
		{3, RoundDown, 1}, {3, RoundUp, 2}, {3, RoundNearInf, 2},
		{-3, RoundDown, -2}, {-3, RoundUp, -1}, {-3, RoundNearInf, -2},
		{avutil.NoPTSValue, RoundUp | RoundPassMinMax, avutil.NoPTSValue},
	} {
		if got := RescaleQRnd(tc.a, half, one, tc.rnd); got != tc.want {
			t.Errorf("RescaleQRnd(%d, 1/2, 1/1, %d) = %d, want %d", tc.a, tc.rnd, got, tc.want)
		}
	}
	if got := RescaleQ(1001, NewRational(1, 24000), NewRational(1, 1000)); got != 42 {
		t.Errorf("RescaleQ(1001, 1/24000, 1/1000) = %d, want 42", got)
	}
}

// av_rescale_delta keeps 1024-sample audio frames contiguous at 48 kHz
// when their pts arrive rounded to milliseconds, and re-anchors on a jump.
func TestRescaleDeltaKeepsAudioContinuousAcrossJitter(t *testing.T) {
	if err := Init(); err != nil {
		t.Skipf("FFmpeg libraries not available: %v", err)
	}
	ms, sr := NewRational(1, 1000), NewRational(1, 48000)
	last := avutil.NoPTSValue
	for i, tc := range []struct{ in, want int64 }{
		{0, 0}, {21, 1024}, {43, 2048}, {64, 3072}, {85, 4096},
		{1000, 48000},
	} {
		if got := RescaleDelta(ms, tc.in, sr, 1024, &last, sr); got != tc.want {
			t.Fatalf("frame %d: pts %d ms rescaled to %d, want %d", i, tc.in, got, tc.want)
		}
	}
	if last != 49024 {
		t.Errorf("last = %d after the jump, want 49024", last)
	}
	last = 0
	if got := RescaleDelta(sr, 1024, sr, 1024, &last, sr); got != 1024 || last != 2048 {
		t.Errorf("same time base: got %d, last %d; want 1024, 2048", got, last)
	}
}
