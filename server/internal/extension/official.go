package extension

import "strings"

// OfficialMarket is the market every hub has without adding it (docs/extensions/extensions.md "Official market", #2351): the public
// repository nwverse/persona-extensions. Its key is the repository's market.pub, pinned here rather than trusted on
// first use.
type OfficialMarket struct {
	Spec       string
	SigningKey string
}

// Official is the official market; a zero OfficialMarket means none (tests).
var Official = OfficialMarket{
	Spec:       "github:nwverse/persona-extensions",
	SigningKey: "ed25519:hcYSzYZjIC0RZYSnQFf9WQvtdFwRoH/d1kILFWNkNmc=",
}

// Is whether spec is this market's repository, however it was written (github:…, https://github.com/…, git:…, with
// or without a branch): one the owner added by hand is the official one, not a second copy.
func (o OfficialMarket) Is(spec string) bool {
	if o.Spec == "" {
		return false
	}
	want, err := ParseMarket(o.Spec)
	if err != nil {
		return false
	}
	got, err := ParseMarket(spec)
	if err != nil {
		return false
	}
	if got.Kind == "github" {
		return strings.EqualFold(got.Owner, want.Owner) && strings.EqualFold(got.Repo, want.Repo)
	}
	if got.Kind == "git" {
		// host/path of any of its forms: https://host/path, ssh://user@host/path, user@host:path.
		u := strings.ToLower(got.URL)
		if _, rest, ok := strings.Cut(u, "://"); ok {
			u = rest
		} else {
			u = strings.Replace(u, ":", "/", 1)
		}
		if _, rest, ok := strings.Cut(u, "@"); ok {
			u = rest
		}
		return strings.TrimSuffix(strings.TrimSuffix(u, "/"), ".git") == strings.ToLower("github.com/"+want.Owner+"/"+want.Repo)
	}
	return false
}
