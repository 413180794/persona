package hub

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/nwverse/persona/server/internal/identity"
)

// How a new manager is installed (docs/managers.md, "Install commands"): the app shows the commands that come with its install
// code. `curl -fsSL <hub>/i/<code> | sh` gets a script from the hub that finds or downloads personad, trades the code
// (`personad manager join … --install`) and installs the manager as a service of that computer; `docker run …` runs
// it as a container of the hub's image. GET /i/<code> needs no credential (the code is it) and does not use the code
// up: only the join does. Unknown or used codes count towards the source's limit, as at the join.

// installScriptPath is where the script is: /i/<code>.
const installScriptPath = "/i/"

// managerAddress is the hub as a manager installed from request r reaches it: the address r came to (it reached the
// hub from where the command runs), https when it came over TLS or through a proxy that says so; fp is the hub's TLS
// pin when r came to its own TLS port (its self-signed certificate), else "".
func (h *Hub) managerAddress(r *http.Request) (base, fp string) {
	scheme := "http"
	if r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
		scheme = "https"
	}
	if r.TLS != nil {
		_, fp = h.tlsPin()
	}
	host := r.Host
	if host == "" {
		return strings.TrimRight(h.cfg.PublicURL, "/"), ""
	}
	return scheme + "://" + host, fp
}

// managerCommands are the commands that install manager id with code, from where r came: service (a computer, the
// hub's own included), join (one that has personad already), docker (when the hub knows its image). The image brings
// no Postgres: the docker command's --database-url is a placeholder the user replaces with a database of theirs.
func (h *Hub) managerCommands(r *http.Request, id, code string) map[string]string {
	base, fp := h.managerAddress(r)
	curl := "curl -fsSL"
	join := fmt.Sprintf("personad manager join %s %s --install", shellWord(base), code)
	if fp != "" {
		curl += " -k --pinnedpubkey " + shellWord("sha256//"+curlPin(fp))
		join += " --fingerprint " + fp
	}
	out := map[string]string{
		"service": fmt.Sprintf("%s %s | sh", curl, shellWord(base+installScriptPath+code)),
		"join":    join,
	}
	image := identity.Getenv("JARVIS_IMAGE")
	if image == "" {
		image = h.build.Image
	}
	if image != "" {
		dir := "/home/jarvis/.jarvis/managers/" + id
		docker := fmt.Sprintf("docker run -d --name persona-manager-%s --restart unless-stopped -v persona-manager-%s:/home/jarvis/.jarvis %s manager run --dir %s --join %s %s --database-url 'postgres://USER:PASSWORD@HOST:5432/DBNAME'",
			id, id, shellWord(image), dir, shellWord(base), code)
		if fp != "" {
			docker += " --fingerprint " + fp
		}
		out["docker"] = docker
	}
	return out
}

// curlPin is a pin (base64url of the key's sha256, no padding) as curl's --pinnedpubkey wants it (standard base64).
func curlPin(fp string) string {
	b, err := base64.RawURLEncoding.DecodeString(fp)
	if err != nil {
		return fp
	}
	return base64.StdEncoding.EncodeToString(b)
}

// shellWord quotes s for sh unless it is plain.
func shellWord(s string) string {
	if s != "" && strings.Trim(s, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789/._-:@") == "" {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// managerInstallRoutes mounts GET /i/<code> beside the join, outside the apps' auth.
func (h *Hub) managerInstallRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /i/{code}", func(w http.ResponseWriter, r *http.Request) {
		if h.sourceOf(r) == sourcePublic && r.TLS == nil && !h.cfg.Security.AllowPublicPlaintext {
			h.securityError(w, r, http.StatusForbidden, "tls_required", 0) // the code would cross the internet in the clear
			return
		}
		key := authSourceKey(r)
		if retry := h.devtok.failures.retry(key, time.Now(), h.cfg.Security); retry > 0 {
			retryAfter(w, retry)
			h.securityError(w, r, http.StatusTooManyRequests, "rate_limited", 0)
			return
		}
		code := r.PathValue("code")
		s := h.managerStore()
		if s == nil {
			http.Error(w, "no database", http.StatusServiceUnavailable)
			return
		}
		if !installCodeShape.MatchString(code) {
			h.devtok.failures.fail(key, time.Now(), h.cfg.Security)
			http.Error(w, "echo 'persona: this install code is unknown, expired or used already' >&2; exit 1", http.StatusNotFound)
			return
		}
		c, err := s.ManagerInstallCodeByHash(r.Context(), hashToken(code))
		if err != nil {
			http.Error(w, "the hub's database did not answer", http.StatusServiceUnavailable)
			return
		}
		if c == nil {
			h.devtok.failures.fail(key, time.Now(), h.cfg.Security)
			http.Error(w, "echo 'persona: this install code is unknown, expired or used already: get a new one in the app' >&2; exit 1", http.StatusNotFound)
			return
		}
		base, fp := h.managerAddress(r)
		w.Header().Set("Content-Type", "text/x-shellscript; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write([]byte(managerInstallScript(managerScript{ID: c.ManagerID, Name: c.Name, Hub: base, Code: code,
			Fingerprint: fp, DL: strings.TrimRight(h.build.UpdateURL, "/")})))
	})
}

// managerScript is what the install script is made of.
type managerScript struct {
	ID, Name, Hub, Code, Fingerprint string
	DL                               string // where the publisher's binaries are (the build's update URL); "" none
}

// managerInstallScript is the POSIX sh script GET /i/<code> answers. It uses the personad of this computer (the
// hub's own on the hub machine) or downloads this hub's release the way install.sh does (checked against
// SHA256SUMS), then joins and installs. Arguments after `sh -s --` go to the join (--dir, --database-url).
func managerInstallScript(m managerScript) string {
	q := shellWord
	name := strings.NewReplacer("\n", " ", "\r", " ").Replace(m.Name)
	fp := ""
	if m.Fingerprint != "" {
		fp = "--fingerprint " + m.Fingerprint
	}
	return `#!/bin/sh
# Persona: install the manager ` + q(name) + ` (` + m.ID + `) on this computer and connect it to the hub
# (docs/managers.md). Arguments after sh -s -- go to personad manager join: --dir <directory>, --database-url <url>.
set -eu
HUB=` + q(m.Hub) + `
CODE=` + q(m.Code) + `
DL=` + q(m.DL) + `
fail() { echo "persona manager: $*" >&2; exit 1; }

find_personad() {
	for c in "${PERSONA_HOME:-$HOME/.persona}/bin/jarvisd" "$HOME/.persona/bin/jarvisd" "$HOME/.jarvis/bin/jarvisd"; do
		[ -x "$c" ] && { printf '%s\n' "$c"; return 0; }
	done
	command -v personad 2>/dev/null && return 0
	command -v jarvisd 2>/dev/null && return 0
	return 1
}

BIN="$(find_personad || true)"
if [ -z "$BIN" ]; then
	[ -n "$DL" ] || fail "this computer has no personad, and the hub does not know where to download it: install personad first (docs/release.md), then run this again"
	case "$(uname -s)" in Linux) os=linux ;; Darwin) os=darwin ;; *) fail "no build for $(uname -s)" ;; esac
	case "$(uname -m)" in x86_64 | amd64) arch=amd64 ;; arm64 | aarch64) arch=arm64 ;; *) fail "no build for $(uname -m)" ;; esac
	if command -v curl >/dev/null 2>&1; then fetch() { curl -fsSL "$1" -o "$2"; }
	elif command -v wget >/dev/null 2>&1; then fetch() { wget -q "$1" -O "$2"; }
	else fail "needs curl or wget"; fi
	if command -v sha256sum >/dev/null 2>&1; then sum() { sha256sum "$1" | cut -d' ' -f1; }
	elif command -v shasum >/dev/null 2>&1; then sum() { shasum -a 256 "$1" | cut -d' ' -f1; }
	else fail "needs sha256sum or shasum to check the download"; fi
	tmp="$(mktemp -d)"
	trap 'rm -rf "$tmp"' EXIT
	archive="jarvisd-$os-$arch.gz"
	fetch "$DL/SHA256SUMS" "$tmp/SHA256SUMS" || fail "could not download $DL/SHA256SUMS"
	fetch "$DL/$archive" "$tmp/$archive" || fail "could not download $DL/$archive"
	want="$(awk -v n="$archive" '$2 == n || $2 == "*" n { print $1 }' "$tmp/SHA256SUMS")"
	[ -n "$want" ] && [ "$(sum "$tmp/$archive")" = "$want" ] || fail "checksum mismatch for $archive: not installed"
	dest="${PERSONA_HOME:-$HOME/.persona}/bin"
	mkdir -p "$dest"
	gzip -dc "$tmp/$archive" > "$tmp/jarvisd" && chmod 755 "$tmp/jarvisd" && mv "$tmp/jarvisd" "$dest/jarvisd" || fail "could not unpack $archive"
	ln -sf jarvisd "$dest/personad"
	BIN="$dest/jarvisd"
	echo "personad installed at $BIN"
fi
"$BIN" manager join "$HUB" "$CODE" ` + fp + ` --install "$@"
`
}
