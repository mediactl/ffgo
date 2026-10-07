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
