package hub

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/nwverse/persona/server/internal/identity"
	"github.com/nwverse/persona/server/internal/pgdocker"
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
// hub's own included), join (one that has personad already), docker (when the hub knows its image: the script that
// starts the manager and its own Postgres, two containers).
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
	if h.managerImage() != "" {
		out["docker"] = fmt.Sprintf("%s %s | sh", curl, shellWord(base+installScriptPath+code+"/docker"))
	}
	return out
}

// managerImage is the image a manager's container runs: the one this Docker hub runs, else the one it was built as;
// "" when neither is known.
func (h *Hub) managerImage() string {
	if image := identity.Getenv("JARVIS_IMAGE"); image != "" {
		return image
	}
	return h.build.Image
}

// managerDockerScript is the POSIX sh script GET /i/<code>/docker answers (docs/managers.md, "Containers"): manager id
// in a container beside a Postgres container of its own, on a network of their own. Names: network persona-mgr-<id>,
// containers persona-mgr-<id> and persona-mgr-<id>-pg, volumes persona-mgr-<id>-home and persona-mgr-<id>-data.
//
// The database password is made up by the shell that runs it (the hub never knows it) and goes through the
// environment only (-e NAME with no value): never on a command line, where `ps` and the containers' Cmd would show it.
// It can run again: what is there is kept, and an existing database's password is read back (from its container, or
// from the manager's manager.json in its volume) instead of made up anew, which Postgres would not take.
func managerDockerScript(id, image, base, code, fp string) string {
	q := shellWord
	fpArgs := ""
	if fp != "" {
		fpArgs = " --fingerprint " + fp
	}
	return `#!/bin/sh
# Persona: run the manager ` + id + ` in Docker, beside a Postgres of its own (docs/managers.md, "Containers").
set -eu
N=` + q("persona-mgr-"+id) + `
ID=` + q(id) + `
IMAGE=` + q(image) + `
HUB=` + q(base) + `
CODE=` + q(code) + `
fail() { echo "persona manager: $*" >&2; exit 1; }
command -v docker >/dev/null 2>&1 || fail "needs docker"
docker info >/dev/null 2>&1 || fail "docker is not running, or this user may not use it"
docker network inspect "$N" >/dev/null 2>&1 || docker network create "$N" >/dev/null
pw=
if docker inspect "$N-pg" >/dev/null 2>&1; then
	pw=$(docker inspect -f '{{range .Config.Env}}{{println .}}{{end}}' "$N-pg" | sed -n 's/^POSTGRES_PASSWORD=//p')
elif docker volume inspect "$N-data" >/dev/null 2>&1; then
	# Its database is there from before: the password is the one in its manager.json, when that volume is there too.
	pw=$(docker run --rm -v "$N-home:/home/jarvis/.jarvis" --entrypoint sh "$IMAGE" -c \
		"sed -n 's|.*postgres://` + pgdocker.User + `:\([0-9a-f]*\)@.*|\1|p' /home/jarvis/.jarvis/managers/$ID/manager.json" 2>/dev/null || true)
	[ -n "$pw" ] || fail "the volume $N-data holds a database whose password is gone: remove it (docker volume rm $N-data, its data with it) and run this again"
fi
[ -n "$pw" ] || pw=$(od -An -N24 -tx1 /dev/urandom | tr -d ' \n')
if ! docker inspect "$N-pg" >/dev/null 2>&1; then
	POSTGRES_PASSWORD=$pw docker run -d --name "$N-pg" --network "$N" --restart unless-stopped \
		-v "$N-data:/var/lib/postgresql" -e POSTGRES_USER=` + pgdocker.User + ` -e POSTGRES_DB=` + pgdocker.Database + ` -e POSTGRES_PASSWORD ` + pgdocker.Image + ` >/dev/null
fi
docker start "$N-pg" >/dev/null
if docker inspect "$N" >/dev/null 2>&1; then
	docker start "$N" >/dev/null
	echo "persona manager: $N was there already: started it"
	exit 0
fi
` + databaseEnvName + `="postgres://` + pgdocker.User + `:$pw@$N-pg:5432/` + pgdocker.Database + `?sslmode=disable" docker run -d --name "$N" --network "$N" \
	--restart unless-stopped -v "$N-home:/home/jarvis/.jarvis" -e ` + databaseEnvName + ` "$IMAGE" \
	manager run --dir "/home/jarvis/.jarvis/managers/$ID" --join "$HUB" "$CODE"` + fpArgs + ` >/dev/null
echo "persona manager: started $N and its Postgres $N-pg (volumes $N-home, $N-data); logs: docker logs -f $N"
`
}

// databaseEnvName is the variable a manager's container gets its database URL in (manager.databaseEnv).
const databaseEnvName = "PERSONA_MANAGER_DATABASE_URL"

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
	mux.HandleFunc("GET /i/{code}", func(w http.ResponseWriter, r *http.Request) { h.serveInstallScript(w, r, false) })
	// The same for a container: the manager and its own Postgres (managerDockerScript), when the hub knows its image.
	mux.HandleFunc("GET /i/{code}/docker", func(w http.ResponseWriter, r *http.Request) { h.serveInstallScript(w, r, true) })
}

// serveInstallScript answers GET /i/<code> (docker: …/docker) with its script.
func (h *Hub) serveInstallScript(w http.ResponseWriter, r *http.Request, docker bool) {
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
	script := managerInstallScript(managerScript{ID: c.ManagerID, Name: c.Name, Hub: base, Code: code,
		Fingerprint: fp, DL: strings.TrimRight(h.build.UpdateURL, "/")})
	if docker {
		image := h.managerImage()
		if image == "" {
			http.Error(w, "echo 'persona: this hub does not know which image a manager container runs' >&2; exit 1", http.StatusNotFound)
			return
		}
		script = managerDockerScript(c.ManagerID, image, base, code, fp)
	}
	w.Header().Set("Content-Type", "text/x-shellscript; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte(script))
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
