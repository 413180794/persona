package screen

import "testing"

func TestSecretPrompt(t *testing.T) {
	for _, s := range []string{
		"$ sudo make install\n[sudo] password for me: ",
		"Password:",
		"person@example.com's password: \n",
		"Enter passphrase for key '/home/me/.ssh/id_ed25519':",
		"Password for 'https://me@git.example.com':",
		"Enter PIN for token:",
	} {
		if !SecretPrompt(s) {
			t.Errorf("not a password prompt: %q", s)
		}
	}
	for _, s := range []string{
		"⏺ The password field now validates length.\n❯ ",
		"Password: must be at least 12 characters",
		"Do you want to change the password?\n ❯ 1. Yes\n   2. No",
		"[sudo] password for me:\nSorry, try again.\n$ ",
		"",
	} {
		if SecretPrompt(s) {
			t.Errorf("taken for a password prompt: %q", s)
		}
	}
}
