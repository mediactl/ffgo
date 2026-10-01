package bindings

import (
	"os"
	"testing"

	"github.com/ebitengine/purego"
)

// The owner's workstation has FFmpeg 9.0 and 4.4 installed side by side.
// ffgo must load one whole release -- never libavutil 56 beside libavcodec
// 63 -- and report which.
func TestLoadPicksTheNewestCompleteSet(t *testing.T) {
	if os.Getenv("FFGO_FFMPEG_MAJOR") != "" {
		t.Skip("FFGO_FFMPEG_MAJOR pins the set")
	}
	if err := Load(); err != nil {
		t.Skipf("no FFmpeg on this host: %v", err)
	}
	set, ok := LoadedVersionSet()
	if !ok {
		t.Fatal("loaded, but no version set recorded")
	}
	got := []int{int(avutilVersion() >> 16), int(avcodecVersion() >> 16), int(avformatVersion() >> 16)}
	want := []int{set.AVUtil, set.AVCodec, set.AVFormat}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("loaded majors %v, but set FFmpeg %d is %v: libraries from two versions", got, set.FFmpeg, want)
		}
	}
	t.Logf("loaded FFmpeg %d (%v)", set.FFmpeg, got)
}

// A package that opens its own library (avfilter, swresample, avdevice)
// gets the loaded release's, whatever versions it asks for: its own list
// stopped at FFmpeg 7 and found 4.4's libraries here.
func TestLoadLibraryUsesTheLoadedSetsMajor(t *testing.T) {
	if err := Load(); err != nil {
		t.Skipf("no FFmpeg on this host: %v", err)
	}
	set, _ := LoadedVersionSet()
	for _, tc := range []struct {
		name, versionFunc string
		asked             []int
		want              int
	}{
		{"avfilter", "avfilter_version", []int{10, 9, 8, 7}, set.AVFilter},
		{"swresample", "swresample_version", []int{5, 4, 3}, set.SWResample},
		{"avdevice", "avdevice_version", []int{61, 60, 59, 58}, set.AVDevice},
	} {
		lib, err := LoadLibrary(tc.name, tc.asked)
		if err != nil {
			t.Errorf("%s: %v", tc.name, err)
			continue
		}
		var version func() uint32
		purego.RegisterLibFunc(&version, lib, tc.versionFunc)
		if got := int(version() >> 16); got != tc.want {
			t.Errorf("%s: loaded major %d, the loaded FFmpeg %d's is %d", tc.name, got, set.FFmpeg, tc.want)
		}
	}
}

func TestKnownVersionSetsAreNewestFirst(t *testing.T) {
	for i := 1; i < len(KnownVersionSets); i++ {
		if KnownVersionSets[i-1].FFmpeg <= KnownVersionSets[i].FFmpeg {
			t.Fatalf("sets not newest first at %d", i)
		}
	}
	want := VersionSet{FFmpeg: 9, AVUtil: 61, AVCodec: 63, AVFormat: 63, AVFilter: 12, SWScale: 10, SWResample: 7, AVDevice: 63}
	if KnownVersionSets[0] != want {
		t.Fatalf("newest set is %+v, want %+v", KnownVersionSets[0], want)
	}
}

func TestAnUnknownPinnedReleaseIsAnError(t *testing.T) {
	t.Setenv("FFGO_FFMPEG_MAJOR", "3")
	if _, err := candidateSets(); err == nil {
		t.Fatal("FFGO_FFMPEG_MAJOR=3 accepted")
	}
	t.Setenv("FFGO_FFMPEG_MAJOR", "nine")
	if _, err := candidateSets(); err == nil {
		t.Fatal("FFGO_FFMPEG_MAJOR=nine accepted")
	}
}
