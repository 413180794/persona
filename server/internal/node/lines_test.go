package node

import (
	"bytes"
	"fmt"
	"math/rand/v2"
	"slices"
	"strings"
	"testing"
)

// linesBackWant is what eachLineBack must give for data[lo:hi]: its complete lines shorter than maxLine (with their
// newline), trimmed, the last one first, and where the complete lines end.
func linesBackWant(data []byte, lo, hi int64, maxLine int) ([]string, int64) {
	part := data[lo:hi]
	k := bytes.LastIndexByte(part, '\n')
	if k < 0 {
		return nil, lo
	}
	var out []string
	for _, l := range strings.SplitAfter(string(part[:k+1]), "\n") {
		if l != "" && len(l) <= maxLine {
			out = append(out, strings.TrimSpace(l))
		}
	}
	slices.Reverse(out)
	return out, lo + int64(k) + 1
}

// Read backward in chunks, the lines and where they end are the same as reading the range forward: lines that span
// chunks, lines longer than the limit, a line still being written at the end, a range starting mid-file.
func TestEachLineBack(t *testing.T) {
	rng := rand.New(rand.NewPCG(1, 2))
	for i := range 300 {
		var b bytes.Buffer
		lines := rng.IntN(12)
		for j := range lines {
			n := rng.IntN(3 * backChunk / 2)
			if rng.IntN(4) == 0 {
				n = rng.IntN(40)
			}
			fmt.Fprintf(&b, "%d:%d:%s", i, j, strings.Repeat("x", n))
			if j < lines-1 || rng.IntN(2) == 0 {
				b.WriteByte('\n')
			}
		}
		data := b.Bytes()
		hi := int64(len(data))
		lo := int64(0)
		if k := bytes.IndexByte(data, '\n'); k >= 0 && rng.IntN(3) == 0 {
			lo = int64(k) + 1 // a later read starts where the last one's lines ended
		}
		maxLine := backChunk + rng.IntN(backChunk)
		want, wantEnd := linesBackWant(data, lo, hi, maxLine)
		var got []string
		end := eachLineBack(bytes.NewReader(data), lo, hi, maxLine, func(line []byte) bool {
			got = append(got, string(line))
			return true
		})
		if end != wantEnd || !slices.Equal(got, want) {
			t.Fatalf("case %d [%d, %d) max %d: end %d (want %d), %d lines (want %d)", i, lo, hi, maxLine, end, wantEnd, len(got), len(want))
		}
		// It ends as the forward reader does.
		if fwd := lo + eachLine(bytes.NewReader(data[lo:hi]), func([]byte) {}); fwd != end {
			t.Fatalf("case %d: forward ends at %d, backward at %d", i, fwd, end)
		}
	}
}

// fn returning false stops it: the lines before are not read.
func TestEachLineBackStops(t *testing.T) {
	data := []byte("one\ntwo\nthree\nfour\npartial")
	var got []string
	end := eachLineBack(bytes.NewReader(data), 0, int64(len(data)), usageMaxLine, func(line []byte) bool {
		got = append(got, string(line))
		return string(line) != "three"
	})
	if end != int64(len(data)-len("partial")) || !slices.Equal(got, []string{"four", "three"}) {
		t.Fatalf("end %d, lines %q", end, got)
	}
	if end := eachLineBack(bytes.NewReader(data), 0, 3, usageMaxLine, func([]byte) bool { t.Fatal("a line without its newline"); return true }); end != 0 {
		t.Fatalf("no complete line: end %d", end)
	}
}

// eachLineAt tells where each line is, as withLineAt reads it again.
func TestEachLineAt(t *testing.T) {
	data := []byte("first\n  second  \n\nfourth\nhalf")
	r := bytes.NewReader(data)
	var got []string
	n := eachLineAt(r, func(line []byte, at, size int64) bool {
		withLineAt(r, at, size, func(again []byte) {
			if !bytes.Equal(again, line) {
				t.Fatalf("line at %d+%d reads %q again, was %q", at, size, again, line)
			}
		})
		got = append(got, fmt.Sprintf("%d+%d %s", at, size, line))
		return true
	})
	want := []string{"0+6 first", "6+11 second", "17+1 ", "18+7 fourth"}
	if n != 25 || !slices.Equal(got, want) {
		t.Fatalf("read %d: %q, want 25: %q", n, got, want)
	}
}

func TestLRU(t *testing.T) {
	c := newLRU[string, int](3)
	for i, k := range []string{"a", "b", "c"} {
		c.put(k, i)
	}
	c.get("a")     // a is used again: b is now the oldest
	c.put("d", 3)  // drops b
	c.put("c", 20) // an update, not an addition
	c.put("e", 4)  // drops a: c and d were used since
	c.peek("d")    // peeking does not count as a use
	c.put("f", 5)  // drops d
	if c.len() != 3 {
		t.Fatalf("len %d", c.len())
	}
	for k, want := range map[string]int{"c": 20, "e": 4, "f": 5} {
		if v, ok := c.peek(k); !ok || v != want {
			t.Fatalf("%s = %d %v, want %d", k, v, ok, want)
		}
	}
	for _, k := range []string{"a", "b", "d"} {
		if _, ok := c.get(k); ok {
			t.Fatalf("%s is still there", k)
		}
	}
}
