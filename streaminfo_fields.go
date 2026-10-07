//go:build !ios && !android && (amd64 || arm64)

package ffgo

import "github.com/obinnaokechukwu/ffgo/avcodec"

// ProfileUnknown is AV_PROFILE_UNKNOWN: a stream whose profile is not known.
const ProfileUnknown = -99

// ProfileName is FFmpeg's name for profile p of codec id ("Main 10", "LC",
// "DTS-HD MA"); id is numbered as the loaded release numbers it, as
// StreamInfo.CodecID holds it. It is "" when FFmpeg names none; ffprobe then
// prints the number, unless p is ProfileUnknown.
func ProfileName(id CodecID, p int) string { return avcodec.ProfileName(id, int32(p)) }

// BitsPerSample is the codec's fixed bits per sample (av_get_bits_per_sample),
// as ffprobe prints an audio stream's bits_per_sample: 16 for pcm_s16le, 0
// for a codec without a fixed size such as FLAC.
func BitsPerSample(id CodecID) int { return int(avcodec.BitsPerSample(id)) }
