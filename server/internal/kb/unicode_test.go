package kb

import (
	"strings"
	"testing"
	"time"
)

// Text from macOS (file names, some clipboards) is often decomposed (NFD): "café" as "cafe" + U+0301. A query typed
// precomposed (NFC) must still find it, and the other way round.
const (
	cafeNFC = "café"
	cafeNFD = "café"
)

func TestUnicodeNormalizationFindMatchesSearch(t *testing.T) {
	for _, tc := range []struct{ name, text, query string }{
		{"NFD text, NFC query", cafeNFD, cafeNFC},
		{"NFC text, NFD query", cafeNFC, cafeNFD},
		{"NFD text, NFC upper query", cafeNFD, strings.ToUpper(cafeNFC)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			st, err := NewStore(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			e := &Engine{Store: st}
			c := &Chat{ID: "lark-u", Name: "group", Kind: "group"}
			if err := st.PutChat(c); err != nil {
				t.Fatal(err)
			}
			summary := "Meet at the " + tc.text + " on Friday"
			if err := st.PutSummary(KindChat, c.ID, summary); err != nil {
				t.Fatal(err)
			}
			at := time.Date(2026, 10, 20, 9, 0, 0, 0, time.Local)
			if err := st.AppendMessages(c, []Line{{At: at, Sender: "A", Text: "the " + tc.text + " is closed"}}); err != nil {
				t.Fatal(err)
			}

			check := func(round string) {
				res := e.Find(tc.query, 10)
				if len(res) != 1 || res[0].Entry.ID != c.ID || res[0].Count != 2 {
					t.Fatalf("%s find: %+v", round, res)
				}
				// The lines come back as written: the archive's text, not the folded form.
				for _, h := range res[0].Hits {
					if !strings.Contains(h.Line, tc.text) || h.N < 1 {
						t.Fatalf("%s hit not raw: %+v", round, h)
					}
				}
			}
			// Age the files past findSettle so the cold search keeps their folded text; fresh files are read once and
			// not cached, which would leave the warm search on the cold path.
			if err := ageFiles(st.Dir, time.Hour); err != nil {
				t.Fatal(err)
			}
			check("cold")
			if len(e.find.texts) == 0 {
				t.Fatal("cold find cached no text")
			}
			check("warm") // the second search reads the cached, folded text

			m := e.Matches(KindChat, c.ID, tc.query, 10)
			if len(m) != 2 {
				t.Fatalf("matches: %+v", m)
			}
			if hits := e.Search(tc.query, true, 10); len(hits) != 2 {
				t.Fatalf("search: %+v", hits)
			}
		})
	}
}

func TestFoldIsCaseAndNormalizationInsensitive(t *testing.T) {
	if fold(cafeNFD) != fold(cafeNFC) || fold("CAFÉ") != fold(cafeNFC) || fold("CAFÉ") != fold(cafeNFC) {
		t.Fatalf("fold: %q %q %q", fold(cafeNFD), fold(cafeNFC), fold("CAFÉ"))
	}
	if s := "plain ascii and 中文"; fold(s) != s {
		t.Fatalf("fold changed folded text: %q", fold(s))
	}
}
