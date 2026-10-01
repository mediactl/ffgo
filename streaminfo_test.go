//go:build !ios && !android && (amd64 || arm64)

package ffgo

import (
	"path/filepath"
	"testing"

	"github.com/obinnaokechukwu/ffgo/internal/shim"
)

func TestStreamsReportDispositionLanguageAndLayout(t *testing.T) {
	ffmpegOrSkip(t)
	requireShim(t)
	path := filepath.Join(t.TempDir(), "in.mkv")
	run(t, "ffmpeg", "-hide_banner", "-loglevel", "error", "-y",
		"-f", "lavfi", "-i", "testsrc2=size=128x72:rate=25:duration=1",
		"-f", "lavfi", "-i", "sine=frequency=440:duration=1,aformat=channel_layouts=5.1(side)",
		"-f", "lavfi", "-i", "sine=frequency=880:duration=1,aformat=channel_layouts=7.1",
		"-map", "0", "-map", "1", "-map", "2", "-c:v", "libx264", "-c:a", "flac",
		"-metadata:s:a:0", "language=eng", "-metadata:s:a:1", "language=fre",
		"-metadata:s:a:1", "title=Commentary",
		"-disposition:a:0", "default", "-disposition:a:1", "comment", path)

	d, err := NewDecoder(path)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	s := d.Streams()
	if len(s) != 3 {
		t.Fatalf("streams = %d, want 3", len(s))
	}
	if s[1].Language != "eng" || s[1].ChannelLayout != "5.1(side)" || s[1].Disposition&DispositionDefault == 0 {
		t.Errorf("stream 1 = language %q layout %q disposition %#x; want eng, 5.1(side), default",
			s[1].Language, s[1].ChannelLayout, s[1].Disposition)
	}
	if s[2].Language != "fre" || s[2].Title != "Commentary" || s[2].ChannelLayout != "7.1" ||
		s[2].Disposition&DispositionComment == 0 {
		t.Errorf("stream 2 = language %q title %q layout %q disposition %#x; want fre, Commentary, 7.1, comment",
			s[2].Language, s[2].Title, s[2].ChannelLayout, s[2].Disposition)
	}
	if s[2].Disposition&DispositionDefault != 0 {
		t.Errorf("stream 2 is default (%#x); only stream 1 was asked to be", s[2].Disposition)
	}
}

func TestDispositionConstantsMatchTheHeaders(t *testing.T) {
	requireShim(t)
	for name, v := range map[string]Disposition{
		"AV_DISPOSITION_DEFAULT": DispositionDefault, "AV_DISPOSITION_DUB": DispositionDub,
		"AV_DISPOSITION_ORIGINAL": DispositionOriginal, "AV_DISPOSITION_COMMENT": DispositionComment,
		"AV_DISPOSITION_LYRICS": DispositionLyrics, "AV_DISPOSITION_KARAOKE": DispositionKaraoke,
		"AV_DISPOSITION_FORCED": DispositionForced, "AV_DISPOSITION_HEARING_IMPAIRED": DispositionHearingImpaired,
		"AV_DISPOSITION_VISUAL_IMPAIRED": DispositionVisualImpaired, "AV_DISPOSITION_CLEAN_EFFECTS": DispositionCleanEffects,
		"AV_DISPOSITION_ATTACHED_PIC": DispositionAttachedPic, "AV_DISPOSITION_CAPTIONS": DispositionCaptions,
		"AV_DISPOSITION_DESCRIPTIONS": DispositionDescriptions, "AV_DISPOSITION_METADATA": DispositionMetadata,
	} {
		got, ok := shim.EnumValue(name)
		if !ok {
			t.Errorf("%s: unknown to the shim", name)
			continue
		}
		if got != int(v) {
			t.Errorf("%s = %#x in the headers, ffgo has %#x", name, got, int(v))
		}
	}
}
