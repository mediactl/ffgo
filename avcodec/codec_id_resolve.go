//go:build !ios && !android && (amd64 || arm64)

package avcodec

import (
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/obinnaokechukwu/ffgo/internal/bindings"
)

// codecNames are FFmpeg's names for ffgo's CodecID constants. AVCodecID
// values move between releases (AV_CODEC_ID_HEVC is 173 through FFmpeg 7
// and 172 in FFmpeg 9; VP9, AV1, WebP and others moved too, and some
// constants never matched any release), so ffgo resolves a constant by its
// name in the loaded release wherever an ID goes into FFmpeg.
var codecNames = map[CodecID]string{
	CodecIDMPEG1VIDEO: "mpeg1video", CodecIDMPEG2VIDEO: "mpeg2video", CodecIDH261: "h261", CodecIDH263: "h263",
	CodecIDRV10: "rv10", CodecIDRV20: "rv20", CodecIDMJPEG: "mjpeg", CodecIDMJPEGB: "mjpegb", CodecIDLJPEG: "ljpeg",
	CodecIDSP5X: "sp5x", CodecIDJPEGLS: "jpegls", CodecIDMPEG4: "mpeg4", CodecIDRAWVIDEO: "rawvideo",
	CodecIDMSMPEG4V1: "msmpeg4v1", CodecIDMSMPEG4V2: "msmpeg4v2", CodecIDMSMPEG4V3: "msmpeg4v3",
	CodecIDWMV1: "wmv1", CodecIDWMV2: "wmv2", CodecIDH263P: "h263p", CodecIDH263I: "h263i", CodecIDFLV1: "flv1",
	CodecIDSVQ1: "svq1", CodecIDSVQ3: "svq3", CodecIDDVVIDEO: "dvvideo", CodecIDHUFFYUV: "huffyuv", CodecIDCYUV: "cyuv",
	CodecIDH264: "h264", CodecIDINDEO3: "indeo3", CodecIDVP3: "vp3", CodecIDTHEORA: "theora",
	CodecIDVP5: "vp5", CodecIDVP6: "vp6", CodecIDVP7: "vp7", CodecIDVP8: "vp8", CodecIDVP9: "vp9",
	CodecIDHEVC: "hevc", CodecIDAV1: "av1", CodecIDBMP: "bmp", CodecIDGIF: "gif", CodecIDTIFF: "tiff",
	CodecIDWEBP:     "webp",
	CodecIDPCMS16LE: "pcm_s16le", CodecIDPCMS16BE: "pcm_s16be", CodecIDPCMU16LE: "pcm_u16le", CodecIDPCMU16BE: "pcm_u16be",
	CodecIDPCMS8: "pcm_s8", CodecIDPCMU8: "pcm_u8", CodecIDMP2: "mp2", CodecIDMP3: "mp3", CodecIDAAC: "aac",
	CodecIDAC3: "ac3", CodecIDDTS: "dts", CodecIDVORBIS: "vorbis", CodecIDFLAC: "flac", CodecIDOPUS: "opus",
	CodecIDALAC: "alac", CodecIDTTF: "ttf", CodecIDBinData: "bin_data",
}

// CodecIDPNG shares VP6's value (61) in ffgo, so it cannot be a map key
// beside it; PNG's real ID is 61 in every release ffgo supports, and VP6's
// is not, so the value keeps meaning PNG when resolved.
func init() { delete(codecNames, CodecIDVP6) }

var (
	descriptorByNameOnce       sync.Once
	avcodecDescriptorGetByName func(name string) uintptr
)

func loadDescriptorByName() {
	descriptorByNameOnce.Do(func() {
		lib := bindings.LibAVCodec()
		if lib == 0 {
			return
		}
		defer func() { _ = recover() }()
		purego.RegisterLibFunc(&avcodecDescriptorGetByName, lib, "avcodec_descriptor_get_by_name")
	})
}

// CodecIDByName is the loaded release's AVCodecID for an FFmpeg codec name
// ("hevc", "eac3", "hdmv_pgs_subtitle"), or CodecIDNone.
func CodecIDByName(name string) CodecID {
	loadDescriptorByName()
	if avcodecDescriptorGetByName == nil {
		return CodecIDNone
	}
	d := avcodecDescriptorGetByName(name)
	if d == 0 {
		return CodecIDNone
	}
	// AVCodecDescriptor.id is its first member in every release.
	return CodecID(*(*int32)(unsafe.Pointer(d)))
}

// Resolve is ffgo's constant id as the loaded release numbers it; an id
// ffgo has no name for is returned as it is.
func Resolve(id CodecID) CodecID {
	name, ok := codecNames[id]
	if !ok {
		return id
	}
	if r := CodecIDByName(name); r != CodecIDNone {
		return r
	}
	return id
}
