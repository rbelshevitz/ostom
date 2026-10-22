package gopst

import (
	"bytes"
	"testing"
)

func TestMboxLineWriterEscapesFromLineOnly(t *testing.T) {
	var got bytes.Buffer
	w := newMboxLineWriter(&got)
	if _, err := w.Write([]byte("From sender\r\nbody\r\nFrom another\r\n")); err != nil {
		t.Fatal(err)
	}
	want := ">From sender\r\nbody\r\n>From another\r\n"
	if got.String() != want {
		t.Fatalf("got %q, want %q", got.String(), want)
	}
}

func TestMboxLineWriterDoesNotEscapeFromHeader(t *testing.T) {
	var got bytes.Buffer
	w := newMboxLineWriter(&got)
	if _, err := w.Write([]byte("From: sender\r\n")); err != nil {
		t.Fatal(err)
	}
	if got.String() != "From: sender\r\n" {
		t.Fatalf("got %q", got.String())
	}
}

func TestReportErrorsAreBounded(t *testing.T) {
	var report Report
	for index := 0; index < reportErrorLimit+17; index++ {
		appendReportError(&report, "synthetic error")
	}
	if len(report.Errors) != reportErrorLimit || report.ErrorsOmitted != 17 {
		t.Fatalf("unexpected bounded errors: kept=%d omitted=%d", len(report.Errors), report.ErrorsOmitted)
	}
}
