//go:build !ios && !android && (amd64 || arm64)

package ffgo

import (
	"fmt"
	"sync"
	"unsafe"

	"github.com/obinnaokechukwu/ffgo/avcodec"
	"github.com/obinnaokechukwu/ffgo/avutil"
	"github.com/obinnaokechukwu/ffgo/internal/shim"
)

// FrameSideDataType is an AVFrameSideDataType.
type FrameSideDataType int32

// PacketSideDataType is an AVPacketSideDataType (stream-level side data in
// AVCodecParameters.coded_side_data).
type PacketSideDataType int32

// Side data types are read from the shim's headers, as pixel formats are by
// name: the enums are append-only, but this keeps ffgo from restating them.
func enumFromShim(name string) func() int32 {
	return sync.OnceValue(func() int32 {
		v, ok := shim.EnumValue(name)
		if !ok {
			return -1 // no shim, or one that predates the name: matches no side data
		}
		return int32(v)
	})
}

var (
	frameMasteringDisplay = enumFromShim("AV_FRAME_DATA_MASTERING_DISPLAY_METADATA")
	frameLightLevel       = enumFromShim("AV_FRAME_DATA_CONTENT_LIGHT_LEVEL")
	frameHDRPlus          = enumFromShim("AV_FRAME_DATA_DYNAMIC_HDR_PLUS")
	frameDOVIRPU          = enumFromShim("AV_FRAME_DATA_DOVI_RPU_BUFFER")
	frameDOVIMetadata     = enumFromShim("AV_FRAME_DATA_DOVI_METADATA")
	pktMasteringDisplay   = enumFromShim("AV_PKT_DATA_MASTERING_DISPLAY_METADATA")
	pktLightLevel         = enumFromShim("AV_PKT_DATA_CONTENT_LIGHT_LEVEL")
	pktDOVIConf           = enumFromShim("AV_PKT_DATA_DOVI_CONF")
	pktHDR10Plus          = enumFromShim("AV_PKT_DATA_DYNAMIC_HDR10_PLUS")
)

// FrameSideMasteringDisplay carries an AVMasteringDisplayMetadata (HDR10).
func FrameSideMasteringDisplay() FrameSideDataType { return FrameSideDataType(frameMasteringDisplay()) }

// FrameSideContentLightLevel carries an AVContentLightMetadata (MaxCLL, MaxFALL).
func FrameSideContentLightLevel() FrameSideDataType { return FrameSideDataType(frameLightLevel()) }

// FrameSideHDRPlus carries HDR10+ dynamic metadata.
func FrameSideHDRPlus() FrameSideDataType { return FrameSideDataType(frameHDRPlus()) }

// FrameSideDOVIRPU carries a Dolby Vision RPU as found in the bitstream.
func FrameSideDOVIRPU() FrameSideDataType { return FrameSideDataType(frameDOVIRPU()) }

// FrameSideDOVIMetadata carries parsed Dolby Vision metadata.
func FrameSideDOVIMetadata() FrameSideDataType { return FrameSideDataType(frameDOVIMetadata()) }

// PacketSideMasteringDisplay is stream-level AVMasteringDisplayMetadata.
func PacketSideMasteringDisplay() PacketSideDataType {
	return PacketSideDataType(pktMasteringDisplay())
}

// PacketSideContentLightLevel is stream-level AVContentLightMetadata.
func PacketSideContentLightLevel() PacketSideDataType { return PacketSideDataType(pktLightLevel()) }

// PacketSideDOVIConf is a stream's Dolby Vision configuration record.
func PacketSideDOVIConf() PacketSideDataType { return PacketSideDataType(pktDOVIConf()) }

// PacketSideHDR10Plus is HDR10+ dynamic metadata at packet level (FFmpeg 7.0+).
func PacketSideHDR10Plus() PacketSideDataType { return PacketSideDataType(pktHDR10Plus()) }

// SideData is a copy of the frame's side data of type t.
func (f Frame) SideData(t FrameSideDataType) ([]byte, bool) {
	if t < 0 {
		return nil, false
	}
	return shim.FrameSideData(f.ptr, int32(t))
}

// RemoveSideData removes the frame's side data of each type given.
func (f Frame) RemoveSideData(types ...FrameSideDataType) {
	for _, t := range types {
		if t >= 0 {
			avutil.FrameRemoveSideData(f.ptr, int32(t))
		}
	}
}

// AddSideData attaches a copy of data to the frame as side data of type t,
// replacing an entry of that type.
func (f Frame) AddSideData(t FrameSideDataType, data []byte) error {
	if !shim.SideDataAvailable() {
		return ErrShimRequired
	}
	if t < 0 {
		return fmt.Errorf("%w: frame side data type", ErrNotSupported)
	}
	if ret := shim.FrameAddSideData(f.ptr, int32(t), data); ret < 0 {
		return avutil.NewError(ret, "av_frame_new_side_data")
	}
	return nil
}

// StreamSideData is a copy of a stream's coded side data of type t (its
// codec parameters' coded_side_data); false before FFmpeg 6.1.
func StreamSideData(par avcodec.Parameters, t PacketSideDataType) ([]byte, bool) {
	if t < 0 {
		return nil, false
	}
	b, ok, err := shim.CodecParSideData(unsafe.Pointer(par), int32(t))
	if err != nil {
		return nil, false
	}
	return b, ok
}

// SetStreamSideData sets a stream's coded side data of type t to a copy of
// data, replacing an entry of that type. ErrNotSupported before FFmpeg 6.1.
func SetStreamSideData(par avcodec.Parameters, t PacketSideDataType, data []byte) error {
	if !shim.SideDataAvailable() {
		return ErrShimRequired
	}
	if t < 0 {
		return fmt.Errorf("%w: stream side data type", ErrNotSupported)
	}
	return sideDataErr(shim.CodecParSetSideData(unsafe.Pointer(par), int32(t), data), "av_packet_side_data_new")
}

// addDecodedSideData gives an encoder side data it reads before its first
// frame (AVCodecContext.decoded_side_data). ErrNotSupported before FFmpeg 7.0.
func addDecodedSideData(ctx avcodec.Context, t FrameSideDataType, data []byte) error {
	if !shim.SideDataAvailable() {
		return ErrShimRequired
	}
	if t < 0 {
		return fmt.Errorf("%w: frame side data type", ErrNotSupported)
	}
	return sideDataErr(shim.CodecCtxAddDecodedSideData(unsafe.Pointer(ctx), int32(t), data), "av_frame_side_data_new")
}

func sideDataErr(ret int32, op string) error {
	switch {
	case ret == -38: // AVERROR(ENOSYS): the shim's headers predate the API
		return ErrNotSupported
	case ret < 0:
		return avutil.NewError(ret, op)
	}
	return nil
}

// streamSideDataCount is how many coded side data entries par has.
func streamSideDataCount(par avcodec.Parameters) int {
	return shim.CodecParNbSideData(unsafe.Pointer(par))
}
