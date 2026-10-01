//go:build !ios && !android && (amd64 || arm64)

package bindings

import (
	"fmt"
	"os"
	"strconv"
)

// VersionSet is one FFmpeg release's library majors. Libraries of different
// releases must never be loaded into one process: their structs differ, and
// a binding resolved against one release reads the other's memory.
type VersionSet struct {
	FFmpeg                                                   int
	AVUtil, AVCodec, AVFormat, AVFilter, SWScale, SWResample int
	AVDevice                                                 int
}

// KnownVersionSets are the FFmpeg releases ffgo loads, newest first. FFmpeg
// 9's majors were read from 9.0.1's sonames; 4-8 from their release notes.
var KnownVersionSets = []VersionSet{
	{FFmpeg: 9, AVUtil: 61, AVCodec: 63, AVFormat: 63, AVFilter: 12, SWScale: 10, SWResample: 7, AVDevice: 63},
	{FFmpeg: 8, AVUtil: 60, AVCodec: 62, AVFormat: 62, AVFilter: 11, SWScale: 9, SWResample: 6, AVDevice: 62},
	{FFmpeg: 7, AVUtil: 59, AVCodec: 61, AVFormat: 61, AVFilter: 10, SWScale: 8, SWResample: 5, AVDevice: 61},
	{FFmpeg: 6, AVUtil: 58, AVCodec: 60, AVFormat: 60, AVFilter: 9, SWScale: 7, SWResample: 4, AVDevice: 60},
	{FFmpeg: 5, AVUtil: 57, AVCodec: 59, AVFormat: 59, AVFilter: 8, SWScale: 6, SWResample: 4, AVDevice: 59},
	{FFmpeg: 4, AVUtil: 56, AVCodec: 58, AVFormat: 58, AVFilter: 7, SWScale: 5, SWResample: 3, AVDevice: 58},
}

var loadedSet *VersionSet

// LoadedVersionSet is the release Load chose.
func LoadedVersionSet() (VersionSet, bool) {
	if loadedSet == nil {
		return VersionSet{}, false
	}
	return *loadedSet, true
}

// major is the set's major for an optional library that packages open with
// LoadLibrary, or 0 for a name the set does not cover.
func (s VersionSet) major(name string) int {
	switch name {
	case "avfilter":
		return s.AVFilter
	case "swresample":
		return s.SWResample
	case "swscale":
		return s.SWScale
	case "avdevice":
		return s.AVDevice
	}
	return 0
}

// candidateSets is KnownVersionSets, or the one release FFGO_FFMPEG_MAJOR
// pins.
func candidateSets() ([]VersionSet, error) {
	pin := os.Getenv("FFGO_FFMPEG_MAJOR")
	if pin == "" {
		return KnownVersionSets, nil
	}
	major, err := strconv.Atoi(pin)
	if err != nil {
		return nil, fmt.Errorf("ffgo: FFGO_FFMPEG_MAJOR=%q is not a number", pin)
	}
	for _, s := range KnownVersionSets {
		if s.FFmpeg == major {
			return []VersionSet{s}, nil
		}
	}
	return nil, fmt.Errorf("ffgo: FFGO_FFMPEG_MAJOR=%d is not a known FFmpeg release", major)
}
