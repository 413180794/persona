package mail

import "testing"

func TestReadableText(t *testing.T) {
	got := ReadableText("<html><style>p{color:red}</style><body><p>Hi Bob,\n  thanks</p><p>Two&nbsp;things:</p><ul><li>one</li><li>two</li></ul>Bye<br>Ann</body></html>")
	want := "Hi Bob, thanks\nTwo things:\n\n• one\n• two\nBye\nAnn"
	if got != want {
		t.Fatalf("got %q\nwant %q", got, want)
	}
}

// stripHTML matched tags in strings.ToLower(s) at s's byte offsets, but lowering can change a string's length:
// U+0130 (2 bytes) and the Kelvin sign (3 bytes) lower to one byte, an invalid byte grows into U+FFFD's three. A
// shorter copy panicked (slice bounds out of range) inside the IMAP fetch goroutine; a longer one let a <style>
// block's CSS through as text.
func TestStripHTMLWhenLoweringChangesLength(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"<p>İstanbul</p><p>示例用户</p>", "İstanbul 示例用户 "},
		{"<p>5 K</p>", "5 K "},
		{"\xff<style>body{color:red}</style>Hello", "\xffHello"},
		{"<STYLE>p{}</STYLE>İ<SCRIPT>x()</SCRIPT>ok", "İok"},
	} {
		if got := stripHTML(c.in); got != c.want {
			t.Errorf("stripHTML(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
