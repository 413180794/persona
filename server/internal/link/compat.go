package link

import "strings"

// Schemes identify a versioned pairing contract, not a different credential.
const legacyPairScheme = "jarvis"

func pairScheme(s string) bool { return s == "persona" || s == legacyPairScheme }

// PairScheme says whether s is a pairing link's scheme, the current one or the baseline hubs' (case does not matter).
func PairScheme(s string) bool { return pairScheme(strings.ToLower(s)) }

// PairSchemes are the schemes pairing links come in, the current one first: what the Windows tray asks to open.
func PairSchemes() []string { return []string{"persona", legacyPairScheme} }

// LegacyPairLink projects a canonical URL onto the baseline input spelling. It
// preserves the exact query and one-time code; it never makes a second invitation.
func LegacyPairLink(raw string) string {
	if rest, ok := strings.CutPrefix(raw, "persona://"); ok {
		return legacyPairScheme + "://" + rest
	}
	return raw
}
