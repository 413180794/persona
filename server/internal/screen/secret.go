package screen

import (
	"regexp"
	"strings"
)

// secretRE are password prompts as the last line of a screen. Deliberately narrow (after Ovis's secret-prompt.ts):
// prose that mentions passwords must not turn a card into a password field.
var secretRE = []*regexp.Regexp{
	regexp.MustCompile(`(?i)^\[sudo\] password for [^:\s]+:$`),
	// su; ssh's me@host's password:
	regexp.MustCompile(`(?i)^(?:\S+'s )?password:$`),
	// ssh keys, gpg
	regexp.MustCompile(`(?i)^enter passphrase(?: for (?:key )?.+)?:$`),
	// hardware tokens
	regexp.MustCompile(`(?i)^(?:enter )?(?:pin|passcode)(?: for .+)?:$`),
	// git over https: Password for 'https://me@host':
	regexp.MustCompile(`(?i)^password for (?:'[^']+'|[^\s:']+):$`),
	regexp.MustCompile(`(?i)^\S+@\S+ password:$`),
}

// SecretPrompt: the screen ends in a password prompt, where what is typed must not be kept or logged.
func SecretPrompt(plain string) bool {
	lines := strings.Split(strings.ReplaceAll(plain, "\r", ""), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		t := strings.TrimSpace(strings.ReplaceAll(lines[i], " ", " "))
		if t == "" {
			continue
		}
		for _, re := range secretRE {
			if re.MatchString(t) {
				return true
			}
		}
		return false
	}
	return false
}
