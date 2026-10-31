package mail

import "testing"

func TestReadableText(t *testing.T) {
	got := ReadableText("<html><style>p{color:red}</style><body><p>Hi Bob,\n  thanks</p><p>Two&nbsp;things:</p><ul><li>one</li><li>two</li></ul>Bye<br>Ann</body></html>")
	want := "Hi Bob, thanks\nTwo things:\n\n• one\n• two\nBye\nAnn"
	if got != want {
		t.Fatalf("got %q\nwant %q", got, want)
	}
}
