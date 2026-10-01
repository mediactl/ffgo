//go:build !ios && !android && (amd64 || arm64)

package shim

import (
	"debug/elf"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/obinnaokechukwu/ffgo/internal/bindings"
)

// ffmpegSoname matches a linked FFmpeg library: libavcodec.so.63.
var ffmpegSoname = regexp.MustCompile(`^lib(avutil|avcodec|avformat|avdevice|avfilter|swscale|swresample)\.so\.(\d+)$`)

// shimMatchesRelease reports whether the shim at path was linked against
// set's FFmpeg libraries, from its DT_NEEDED sonames, and why not. A shim
// linked against another release would load that release's libraries
// beside set's and read structs at that release's offsets. A file that is
// not ELF (macOS, Windows) cannot be checked this way and is accepted.
func shimMatchesRelease(path string, set bindings.VersionSet) (bool, string) {
	f, err := elf.Open(path)
	if err != nil {
		return true, ""
	}
	defer f.Close()
	libs, err := f.ImportedLibraries()
	if err != nil {
		return true, ""
	}
	want := map[string]int{
		"avutil": set.AVUtil, "avcodec": set.AVCodec, "avformat": set.AVFormat,
		"avdevice": set.AVDevice, "avfilter": set.AVFilter, "swscale": set.SWScale,
		"swresample": set.SWResample,
	}
	var wrong []string
	for _, lib := range libs {
		m := ffmpegSoname.FindStringSubmatch(lib)
		if m == nil {
			continue
		}
		major, _ := strconv.Atoi(m[2])
		if w := want[m[1]]; w != 0 && major != w {
			wrong = append(wrong, fmt.Sprintf("%s (FFmpeg %d's is lib%s.so.%d)", lib, set.FFmpeg, m[1], w))
		}
	}
	if len(wrong) > 0 {
		return false, fmt.Sprintf("%s links %s", path, strings.Join(wrong, ", "))
	}
	return true, ""
}
