//go:build !ios && !android && (amd64 || arm64)

package ffgo

import "github.com/obinnaokechukwu/ffgo/avutil"

// Rounding is FFmpeg's AVRounding.
type Rounding int32

const (
	RoundDown       Rounding = 2    // AV_ROUND_DOWN: toward -infinity
	RoundUp         Rounding = 3    // AV_ROUND_UP: toward +infinity
	RoundNearInf    Rounding = 5    // AV_ROUND_NEAR_INF: to nearest, halfway away from zero
	RoundPassMinMax Rounding = 8192 // AV_ROUND_PASS_MINMAX: INT64_MIN and INT64_MAX pass through unchanged
)

// RescaleQRnd is av_rescale_q_rnd: a in time base bq expressed in cq, through
// av_rescale_rnd (av_rescale_q_rnd takes AVRationals by value, which purego
// cannot pass on every platform).
func RescaleQRnd(a int64, bq, cq Rational, rnd Rounding) int64 {
	return avutil.RescaleRnd(a, int64(bq.Num)*int64(cq.Den), int64(cq.Num)*int64(bq.Den), int32(rnd))
}

// RescaleQ is av_rescale_q: RescaleQRnd with RoundNearInf.
func RescaleQ(a int64, bq, cq Rational) int64 { return RescaleQRnd(a, bq, cq, RoundNearInf) }

// RescaleDelta is av_rescale_delta (libavutil/mathematics.c), ported because
// it takes AVRationals by value: it rescales inTS from inTB to outTB keeping
// consecutive frames of duration samples (in fsTB) contiguous while the
// rounding allows, tracking the next expected timestamp in *last
// (avutil.NoPTSValue to start). ffmpeg(1)'s audio_ts_process uses it.
func RescaleDelta(inTB Rational, inTS int64, fsTB Rational, duration int, last *int64, outTB Rational) int64 {
	simple := func() int64 {
		*last = RescaleQ(inTS, inTB, fsTB) + int64(duration)
		return RescaleQ(inTS, inTB, outTB)
	}
	if *last == avutil.NoPTSValue || duration == 0 ||
		int64(inTB.Num)*int64(outTB.Den) <= int64(outTB.Num)*int64(inTB.Den) {
		return simple()
	}
	a := RescaleQRnd(2*inTS-1, inTB, fsTB, RoundDown) >> 1
	b := (RescaleQRnd(2*inTS+1, inTB, fsTB, RoundUp) + 1) >> 1
	if *last < 2*a-b || *last > 2*b-a {
		return simple()
	}
	this := min(max(*last, a), b) // av_clip64
	*last = this + int64(duration)
	return RescaleQ(this, fsTB, outTB)
}
