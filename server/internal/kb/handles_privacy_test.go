package kb

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"slices"
	"strings"
	"testing"
)

const (
	handleTestPhone     = "+1 555 010 0199"
	handleTestPhoneE164 = "+15550100199"
	handleTestEmail     = "Zhang@Example.com"
	// Independently calculated HMAC-SHA256 vectors for the fixture key in handles_test.go.
	handleTestPhoneHash = "phone:90f201baa20feef51834fb8d3b1f3d85"
	handleTestEmailHash = "email:8483d49a673c7c9951d64ccec8b26fcc"
)

var (
	handleTestPhoneRun = regexp.MustCompile(`\+?[0-9][0-9[:space:]().-]*`)
	handleTestHash     = regexp.MustCompile(`^(phone|email):[0-9a-f]{32}$`)
)

// Check complete hints, including punctuation/case variants, not short digits that a timestamp or hash can share.
// This is a test-only oracle: it does not change the application's privacy policy or scanner.
func checkHandleText(text string) error {
	lower := strings.ToLower(text)
	if strings.Contains(lower, strings.ToLower(handleTestEmail)) {
		return fmt.Errorf("contains the complete email hint")
	}
	for _, prefix := range []string{"phone:", "email:"} {
		if strings.Contains(lower, prefix) {
			return fmt.Errorf("exposes a hashed hint outside handles.json")
		}
	}
	for _, run := range handleTestPhoneRun.FindAllString(text, -1) {
		digits := strings.Map(func(r rune) rune {
			if r >= '0' && r <= '9' {
				return r
			}
			return -1
		}, run)
		if strings.Contains(digits, strings.TrimPrefix(handleTestPhoneE164, "+")) {
			return fmt.Errorf("contains the complete phone hint")
		}
	}
	return nil
}

// Walk every JSON token, including unknown fields, escaped strings and duplicate keys. Decoding into a Person
// would silently discard a new leaked field; searching serialized bytes would mistake .5550199 for a phone.
func checkHandleJSON(raw []byte) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var walk func() error
	walk = func() error {
		token, err := dec.Token()
		if err != nil {
			return err
		}
		switch v := token.(type) {
		case json.Delim:
			for dec.More() {
				if v == '{' {
					key, err := dec.Token()
					if err != nil {
						return err
					}
					name, ok := key.(string)
					if !ok {
						return fmt.Errorf("non-string object key")
					}
					semantic := strings.ReplaceAll(strings.ToLower(name), "_", "")
					if semantic == "handles" || semantic == "senderhandles" {
						return fmt.Errorf("exposes a handle field")
					}
					if err := checkHandleText(name); err != nil {
						return err
					}
				}
				if err := walk(); err != nil {
					return err
				}
			}
			_, err := dec.Token() // The decoder validates the matching closing delimiter.
			return err
		case string:
			return checkHandleText(v)
		case json.Number:
			// A small count is not a phone. A complete hint written as an integer is still a leak.
			return checkHandleText(v.String())
		}
		return nil
	}
	if err := walk(); err != nil {
		return err
	}
	if _, err := dec.Token(); err != io.EOF {
		return fmt.Errorf("expected one complete JSON value, got %v", err)
	}
	return nil
}

// Only the exact expected hashes may be persisted in the dedicated file, with no duplicates or raw values.
func checkStoredHandles(raw []byte, want []string) error {
	var got []string
	if err := json.Unmarshal(raw, &got); err != nil {
		return err
	}
	for _, value := range got {
		if !handleTestHash.MatchString(value) {
			return fmt.Errorf("invalid stored handle format")
		}
	}
	want = slices.Clone(want)
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		return fmt.Errorf("stored handles differ from the exact HMAC vectors")
	}
	return nil
}

func TestHandlePrivacyOracle(t *testing.T) {
	cases := []struct {
		name string
		json string
		leak bool
	}{
		{"timestamp fragments", `{"last_at":"2026-11-03T12:00:00.5550199Z","last_seen":"2026-11-03T12:00:00.5550199Z"}`, false},
		{"numeric and identifier fragments", `{"messages":555,"pending":199,"id":"0199","other":[null,true,"555"]}`, false},
		{"raw phone in note", `{"note":"+1 555 010 0199"}`, true},
		{"normalized phone", `{"note":"+15550100199"}`, true},
		{"phone without plus", `{"note":"15550100199"}`, true},
		{"punctuated phone in nested array", `{"extra":[{"note":"Call +1 (555) 010-0199."}]}`, true},
		{"numeric complete phone", `{"unknown":15550100199}`, true},
		{"escaped phone", `{"note":"\u002b1\u0020555\u0020010\u00200199"}`, true},
		{"phone in unknown key", `{"+15550100199":"safe"}`, true},
		{"phone masquerading as timestamp", `{"last_seen":"+15550100199"}`, true},
		{"email in nested array", `{"extra":["ZHANG@EXAMPLE.COM"]}`, true},
		{"email in unknown key", `{"zhang@example.com":"safe"}`, true},
		{"hint structure", `{"extra":{"sender_handles":[]}}`, true},
		{"escaped hint structure", `{"sender\u005fhandles":[]}`, true},
		{"camel case hint structure", `{"senderHandles":[]}`, true},
		{"dedicated handles outside file", `{"handles":[]}`, true},
		{"phone hash outside file", `{"note":"` + handleTestPhoneHash + `"}`, true},
		{"email hash outside file", `{"note":"` + handleTestEmailHash + `"}`, true},
		{"duplicate key must not hide phone", `{"note":"+15550100199","note":"safe"}`, true},
		{"malformed JSON", `{"note":`, true},
		{"trailing JSON", `{"note":"safe"} {"note":"+15550100199"}`, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := checkHandleJSON([]byte(tc.json)); (err != nil) != tc.leak {
				t.Fatalf("leak detection = %v, want %v; error: %v", err != nil, tc.leak, err)
			}
		})
	}
	for _, text := range []string{handleTestPhone, handleTestPhoneE164, "Call +1 (555) 010-0199", handleTestEmail} {
		if err := checkHandleText(text); err == nil {
			t.Errorf("text-file hint was not detected")
		}
	}
	if err := checkHandleText("555 messages, identifier 0199, 2026-11-03T12:00:00.5550199Z"); err != nil {
		t.Errorf("short fragments were mistaken for a phone: %v", err)
	}
}

func TestStoredHandlesOracle(t *testing.T) {
	good, err := json.Marshal([]string{handleTestEmailHash, handleTestPhoneHash})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{handleTestPhoneHash, handleTestEmailHash}
	if err := checkStoredHandles(good, want); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{
		`["` + handleTestPhone + `"]`,
		`["` + handleTestPhoneE164 + `"]`,
		`["` + handleTestPhoneHash + `","` + handleTestPhoneHash + `"]`,
		`["` + handleTestPhoneHash + `"]`,
		`["phone:00000000000000000000000000000000","` + handleTestEmailHash + `"]`,
		`["phone:NOTHEX","` + handleTestEmailHash + `"]`,
		`{"handles":["` + handleTestPhoneHash + `"]}`,
	} {
		if err := checkStoredHandles([]byte(bad), want); err == nil {
			t.Errorf("invalid or unexpected stored hints accepted")
		}
	}
}
