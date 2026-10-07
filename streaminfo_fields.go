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

// FieldOrder is a video stream's AVFieldOrder.
type FieldOrder int32

const (
	FieldOrderUnknown     FieldOrder = iota // AV_FIELD_UNKNOWN
	FieldOrderProgressive                   // AV_FIELD_PROGRESSIVE
	FieldOrderTT                            // AV_FIELD_TT: top coded first, top displayed first
	FieldOrderBB                            // AV_FIELD_BB
	FieldOrderTB                            // AV_FIELD_TB: top coded first, bottom displayed first
	FieldOrderBT                            // AV_FIELD_BT
)

// String is ffprobe's name for the field order; "" when unknown, which
// ffprobe prints as "unknown" and its JSON leaves out.
func (f FieldOrder) String() string {
	switch f {
	case FieldOrderProgressive:
		return "progressive"
	case FieldOrderTT:
		return "tt"
	case FieldOrderBB:
		return "bb"
	case FieldOrderTB:
		return "tb"
	case FieldOrderBT:
		return "bt"
	}
	return ""
}

// ChannelOrder is an audio stream's AVChannelOrder.
type ChannelOrder int32

const (
	ChannelOrderUnspec    ChannelOrder = 0 // AV_CHANNEL_ORDER_UNSPEC: a channel count, no positions
	ChannelOrderNative    ChannelOrder = 1 // AV_CHANNEL_ORDER_NATIVE: a layout mask
	ChannelOrderCustom    ChannelOrder = 2 // AV_CHANNEL_ORDER_CUSTOM
	ChannelOrderAmbisonic ChannelOrder = 3 // AV_CHANNEL_ORDER_AMBISONIC
)
