package link

import "strings"

// Schemes identify a versioned pairing contract, not a different credential.
const legacyPairScheme = "jarvis"

func pairScheme(s string) bool { return s == "persona" || s == legacyPairScheme }

// LegacyPairLink projects a canonical URL onto the baseline input spelling. It
// preserves the exact query and one-time code; it never makes a second invitation.
func LegacyPairLink(raw string) string {
	if rest, ok := strings.CutPrefix(raw, "persona://"); ok {
		return legacyPairScheme + "://" + rest
	}
	return raw
}
