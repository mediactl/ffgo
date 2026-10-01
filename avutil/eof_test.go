//go:build !ios && !android && (amd64 || arm64)

package avutil

import (
	"errors"
	"io"
	"testing"
)

// FFmpeg's end of stream is an *Error with AVERROR_EOF, which IsEOF
// recognises; Go callers check errors.Is(err, io.EOF). Both must hold.
func TestFFmpegEOFIsIOEOF(t *testing.T) {
	err := NewError(AVERROR_EOF, "av_read_frame")
	if !IsEOF(err) {
		t.Fatal("IsEOF does not recognise AVERROR_EOF")
	}
	if !errors.Is(err, io.EOF) {
		t.Fatal("errors.Is(AVERROR_EOF, io.EOF) is false")
	}
	if errors.Is(NewError(AVERROR_EAGAIN, "x"), io.EOF) {
		t.Fatal("EAGAIN matched io.EOF")
	}
}
