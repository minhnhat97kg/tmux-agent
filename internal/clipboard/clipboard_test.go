package clipboard

import (
	"strings"
	"testing"
)

// TestOSC52SequenceEmptyString checks that an empty string still produces
// the correct escape frame, with the base64 part in the middle also empty.
func TestOSC52SequenceEmptyString(t *testing.T) {
	got := OSC52Sequence("")
	want := "\x1b]52;c;\x07"
	if got != want {
		t.Fatalf("OSC52Sequence(\"\") = %q, want %q", got, want)
	}
}

// TestOSC52SequenceBase64EncodingRFC4648 checks against the standard RFC
// 4648 test vectors (the same set used in the reference Rust
// implementation's tests) to make sure the base64 encoding (including '='
// padding) is correct.
func TestOSC52SequenceBase64EncodingRFC4648(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"f", "Zg=="},
		{"fo", "Zm8="},
		{"foo", "Zm9v"},
		{"foob", "Zm9vYg=="},
		{"fooba", "Zm9vYmE="},
		{"foobar", "Zm9vYmFy"},
	}
	for _, c := range cases {
		got := OSC52Sequence(c.in)
		want := "\x1b]52;c;" + c.want + "\x07"
		if got != want {
			t.Errorf("OSC52Sequence(%q) = %q, want %q", c.in, got, want)
		}
	}
}

// TestOSC52SequenceCorrectEscapeFormat checks the exact
// ESC ] 5 2 ; c ; <base64> BEL format of OSC 52.
func TestOSC52SequenceCorrectEscapeFormat(t *testing.T) {
	got := OSC52Sequence("foo")
	want := "\x1b]52;c;Zm9v\x07"
	if got != want {
		t.Fatalf("OSC52Sequence(\"foo\") = %q, want %q", got, want)
	}
}

// TestOSC52SequenceSupportsUnicode checks that a multi-byte UTF-8 string
// does not corrupt the escape frame (the prefix/suffix are still correct).
func TestOSC52SequenceSupportsUnicode(t *testing.T) {
	got := OSC52Sequence("こんにちは")
	if !strings.HasPrefix(got, "\x1b]52;c;") {
		t.Fatalf("OSC52Sequence with Unicode characters does not start with the correct frame: %q", got)
	}
	if !strings.HasSuffix(got, "\x07") {
		t.Fatalf("OSC52Sequence with Unicode characters does not end with BEL: %q", got)
	}
}
