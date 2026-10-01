//go:build !ios && !android && (amd64 || arm64)

package shim

import (
	"errors"
	"unsafe"
)

// ErrENOSYS is the shim's answer for an API the loaded release lacks.
var ErrENOSYS = errors.New("ffgo shim: not in this FFmpeg release")

var (
	shimFrameSideData        func(frame uintptr, typ int32, size *uintptr) uintptr
	shimFrameAddSideData     func(frame uintptr, typ int32, data unsafe.Pointer, size uintptr) int32
	shimCodecParSideDataGet  func(par uintptr, typ int32, data *uintptr, size *uintptr) int32
	shimCodecParSideDataSet  func(par uintptr, typ int32, data unsafe.Pointer, size uintptr) int32
	shimCodecParNbSideData   func(par uintptr) int32
	shimCodecCtxAddDecodedSD func(ctx uintptr, typ int32, data unsafe.Pointer, size uintptr) int32
)

const averrorENOSYS = -38

func registerSideDataBindings() {
	registerOptionalLibFunc(&shimFrameSideData, libShim, "ffshim_frame_side_data")
	registerOptionalLibFunc(&shimFrameAddSideData, libShim, "ffshim_frame_add_side_data")
	registerOptionalLibFunc(&shimCodecParSideDataGet, libShim, "ffshim_codecpar_side_data_get")
	registerOptionalLibFunc(&shimCodecParSideDataSet, libShim, "ffshim_codecpar_side_data_set")
	registerOptionalLibFunc(&shimCodecParNbSideData, libShim, "ffshim_codecpar_nb_side_data")
	registerOptionalLibFunc(&shimCodecCtxAddDecodedSD, libShim, "ffshim_codecctx_add_decoded_side_data")
}

// SideDataAvailable reports whether this shim has the side-data functions.
func SideDataAvailable() bool { return loaded && shimFrameSideData != nil }

// FrameSideData is a copy of frame's side data of type typ.
func FrameSideData(frame unsafe.Pointer, typ int32) ([]byte, bool) {
	if !SideDataAvailable() || frame == nil {
		return nil, false
	}
	var size uintptr
	p := shimFrameSideData(uintptr(frame), typ, &size)
	if p == 0 {
		return nil, false
	}
	return copyOut(p, size), true
}

// FrameAddSideData adds a copy of data to frame as side data of type typ,
// replacing an entry of that type; it returns the AVERROR code.
func FrameAddSideData(frame unsafe.Pointer, typ int32, data []byte) int32 {
	if !SideDataAvailable() || frame == nil {
		return averrorENOSYS
	}
	return shimFrameAddSideData(uintptr(frame), typ, ptr(data), uintptr(len(data)))
}

// CodecParSideData is a copy of a stream's coded side data of type typ;
// the error is ErrENOSYS before FFmpeg 6.1.
func CodecParSideData(par unsafe.Pointer, typ int32) ([]byte, bool, error) {
	if !SideDataAvailable() || par == nil {
		return nil, false, ErrENOSYS
	}
	var p, size uintptr
	switch ret := shimCodecParSideDataGet(uintptr(par), typ, &p, &size); {
	case ret == averrorENOSYS:
		return nil, false, ErrENOSYS
	case ret < 0:
		return nil, false, nil
	}
	return copyOut(p, size), true, nil
}

// CodecParSetSideData sets a stream's coded side data of type typ (a copy),
// replacing an entry of that type; it returns the AVERROR code.
func CodecParSetSideData(par unsafe.Pointer, typ int32, data []byte) int32 {
	if !SideDataAvailable() || par == nil {
		return averrorENOSYS
	}
	return shimCodecParSideDataSet(uintptr(par), typ, ptr(data), uintptr(len(data)))
}

// CodecParNbSideData is how many coded side data entries a stream has.
func CodecParNbSideData(par unsafe.Pointer) int {
	if !SideDataAvailable() || par == nil {
		return averrorENOSYS
	}
	return int(shimCodecParNbSideData(uintptr(par)))
}

// CodecCtxAddDecodedSideData gives an encoder side data to read before its
// first frame; it returns the AVERROR code (ENOSYS before FFmpeg 7.0).
func CodecCtxAddDecodedSideData(ctx unsafe.Pointer, typ int32, data []byte) int32 {
	if !SideDataAvailable() || ctx == nil {
		return averrorENOSYS
	}
	return shimCodecCtxAddDecodedSD(uintptr(ctx), typ, ptr(data), uintptr(len(data)))
}

func copyOut(p, size uintptr) []byte {
	out := make([]byte, size)
	if size > 0 {
		copy(out, unsafe.Slice((*byte)(unsafe.Pointer(p)), size))
	}
	return out
}

func ptr(b []byte) unsafe.Pointer {
	if len(b) == 0 {
		return nil
	}
	return unsafe.Pointer(&b[0])
}
