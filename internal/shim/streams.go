//go:build !ios && !android && (amd64 || arm64)

package shim

import "unsafe"

var (
	shimEnumValue                func(name string, out *int32) int32
	shimStreamDisposition        func(st uintptr) int32
	shimStreamSetDisposition     func(st uintptr, d int32)
	shimCodecParChLayoutDescribe func(par uintptr, buf *byte, size uintptr) int32
	shimFrameChLayoutDescribe    func(frame uintptr, buf *byte, size uintptr) int32
)

func registerStreamBindings() {
	registerOptionalLibFunc(&shimEnumValue, libShim, "ffshim_enum_value")
	registerOptionalLibFunc(&shimStreamDisposition, libShim, "ffshim_stream_disposition")
	registerOptionalLibFunc(&shimStreamSetDisposition, libShim, "ffshim_stream_set_disposition")
	registerOptionalLibFunc(&shimCodecParChLayoutDescribe, libShim, "ffshim_codecpar_ch_layout_describe")
	registerOptionalLibFunc(&shimFrameChLayoutDescribe, libShim, "ffshim_frame_ch_layout_describe")
}

// EnumValue is a C enum or macro value ("AV_DISPOSITION_FORCED",
// "AV_FRAME_DATA_DOVI_RPU_BUFFER") as the headers the shim was compiled
// against define it; ok is false without a shim that knows the name.
func EnumValue(name string) (int, bool) {
	if !loaded || shimEnumValue == nil {
		return 0, false
	}
	var v int32
	if shimEnumValue(name, &v) != 0 {
		return 0, false
	}
	return int(v), true
}

// StreamDisposition is an AVStream's disposition flags; 0 without the shim.
func StreamDisposition(st unsafe.Pointer) int32 {
	if !loaded || shimStreamDisposition == nil || st == nil {
		return 0
	}
	return shimStreamDisposition(uintptr(st))
}

// SetStreamDisposition sets an AVStream's disposition flags; ok is false
// without the shim.
func SetStreamDisposition(st unsafe.Pointer, d int32) bool {
	if !loaded || shimStreamSetDisposition == nil || st == nil {
		return false
	}
	shimStreamSetDisposition(uintptr(st), d)
	return true
}

// CodecParChLayoutDescribe is an AVCodecParameters' channel layout name
// ("5.1(side)"); "" without the shim or for a stream with no layout.
func CodecParChLayoutDescribe(par unsafe.Pointer) string {
	if !loaded || shimCodecParChLayoutDescribe == nil || par == nil {
		return ""
	}
	return describe(func(b *byte, n uintptr) int32 { return shimCodecParChLayoutDescribe(uintptr(par), b, n) })
}

// FrameChLayoutDescribe is an audio frame's channel layout name; "" without
// the shim.
func FrameChLayoutDescribe(frame unsafe.Pointer) string {
	if !loaded || shimFrameChLayoutDescribe == nil || frame == nil {
		return ""
	}
	return describe(func(b *byte, n uintptr) int32 { return shimFrameChLayoutDescribe(uintptr(frame), b, n) })
}

func describe(f func(*byte, uintptr) int32) string {
	buf := make([]byte, 128)
	if n := f(&buf[0], uintptr(len(buf))); n <= 0 {
		return ""
	}
	for i, c := range buf {
		if c == 0 {
			return string(buf[:i])
		}
	}
	return string(buf)
}
