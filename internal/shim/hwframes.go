//go:build !ios && !android && (amd64 || arm64)

package shim

import "unsafe"

var (
	shimCodecID           func(codec uintptr) int32
	shimHWFramesNew       func(device uintptr, format, swFormat, width, height, pool int32, out *uintptr) int32
	shimFilterSetHWDevice func(filter, device uintptr) int32
)

func registerHWFramesBindings() {
	registerOptionalLibFunc(&shimCodecID, libShim, "ffshim_codec_id")
	registerOptionalLibFunc(&shimHWFramesNew, libShim, "ffshim_hwframes_new")
	registerOptionalLibFunc(&shimFilterSetHWDevice, libShim, "ffshim_filter_set_hw_device")
}

// CodecID is an AVCodec's codec id, or -1 without the shim.
func CodecID(codec unsafe.Pointer) int {
	if !loaded || shimCodecID == nil {
		return -1
	}
	return int(shimCodecID(uintptr(codec)))
}

// HWFramesNew makes and initialises a GPU frame pool on device; it returns
// the AVBufferRef and the AVERROR code.
func HWFramesNew(device unsafe.Pointer, format, swFormat, width, height, pool int32) (unsafe.Pointer, int32) {
	if !loaded || shimHWFramesNew == nil {
		return nil, averrorENOSYS
	}
	var out uintptr
	ret := shimHWFramesNew(uintptr(device), format, swFormat, width, height, pool, &out)
	return unsafe.Pointer(out), ret
}

// FilterSetHWDevice gives a filter context a hardware device reference.
func FilterSetHWDevice(filter, device unsafe.Pointer) int32 {
	if !loaded || shimFilterSetHWDevice == nil {
		return averrorENOSYS
	}
	return shimFilterSetHWDevice(uintptr(filter), uintptr(device))
}
