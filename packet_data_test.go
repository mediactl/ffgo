//go:build !ios && !android && (amd64 || arm64)

package ffgo

import (
	"bytes"
	"testing"

	"github.com/obinnaokechukwu/ffgo/avcodec"
)

func TestNewPacketFromDataRoundTrips(t *testing.T) {
	if err := Init(); err != nil {
		t.Skipf("no FFmpeg: %v", err)
	}
	p, err := NewPacketFromData([]byte("\x00\x05Hello"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = p.Free() }()
	if got := string(p.Data()); got != "\x00\x05Hello" {
		t.Fatalf("Data() = %q", got)
	}
	if p.Size() != 7 {
		t.Fatalf("Size() = %d", p.Size())
	}
}

func TestCodecParametersTakeACodecIDAndExtradata(t *testing.T) {
	if err := Init(); err != nil {
		t.Skipf("no FFmpeg: %v", err)
	}
	par := avcodec.ParametersAlloc()
	defer avcodec.ParametersFree(&par)
	id := avcodec.CodecIDByName("mov_text")
	if id == 0 {
		t.Fatal("mov_text has no codec id")
	}
	avcodec.SetCodecParCodecID(par, id)
	if err := avcodec.SetCodecParExtradata(par, []byte{1, 2, 3}); err != nil {
		t.Fatal(err)
	}
	if got := avcodec.GetCodecParCodecID(par); got != id {
		t.Fatalf("codec id %v, want %v", got, id)
	}
	if got := avcodec.GetCodecParExtradata(par); !bytes.Equal(got, []byte{1, 2, 3}) {
		t.Fatalf("extradata %v", got)
	}
	// Replacing frees the old buffer and takes the new one.
	if err := avcodec.SetCodecParExtradata(par, []byte{9}); err != nil {
		t.Fatal(err)
	}
	if got := avcodec.GetCodecParExtradata(par); !bytes.Equal(got, []byte{9}) {
		t.Fatalf("extradata after replace %v", got)
	}
}
