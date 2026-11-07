package hub

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writePersona lays out a package in dir/id: the manifest and the files it names, with the bytes given (default "png").
func writePersona(t *testing.T, dir, id, manifest string, files map[string]string) {
	t.Helper()
	root := filepath.Join(dir, id)
	for name, body := range files {
		p := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "persona.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
}

func personaManifestJSON(id string) string {
	return `{"format":1,"id":"` + id + `","name":"豆豆","avatar":{"sizes":{"128":"avatar/128.png"}},` +
		`"dot":{"size":144,"states":{"idle":{"file":"dot/idle.png","fps":6},"working":{"file":"dot/working.png"}}},` +
		`"colors":{"light":{"accent":"#8A5A00"},"dark":{"accent":"#E0A400"}}}`
}

func personaFilesOK() map[string]string {
	return map[string]string{"avatar/128.png": "png-a", "dot/idle.png": "png-i", "dot/working.png": "png-w"}
}

func getPersonas(t *testing.T, srv *httptest.Server, token string) []personaInfo {
	t.Helper()
	req, _ := http.NewRequest("GET", srv.URL+"/api/v1/personas", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("list: %v %v", err, resp.Status)
	}
	defer resp.Body.Close()
	var out struct {
		Personas []personaInfo `json:"personas"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	return out.Personas
}

func getPersonaFile(t *testing.T, srv *httptest.Server, token, p string) (int, string) {
	t.Helper()
	req, _ := http.NewRequest("GET", srv.URL+"/api/v1/personas/"+p, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

func TestPersonasListOnlyWholePackages(t *testing.T) {
	h := newTestHub(t)
	dir := h.personasDir()
	writePersona(t, dir, "doudou", personaManifestJSON("doudou"), personaFilesOK())
	// Each of these is left out: a file the manifest names is missing, the id is not the directory's, the built-ins' ids,
	// no idle dot, a path out of the package, a format the hub does not know.
	missing := personaFilesOK()
	delete(missing, "dot/working.png")
	writePersona(t, dir, "missing", personaManifestJSON("missing"), missing)
	writePersona(t, dir, "renamed", personaManifestJSON("other"), personaFilesOK())
	writePersona(t, dir, "persona", personaManifestJSON("persona"), personaFilesOK())
	writePersona(t, dir, "persona-visor", personaManifestJSON("persona-visor"), personaFilesOK())
	writePersona(t, dir, "persona-circuit", personaManifestJSON("persona-circuit"), personaFilesOK())
	writePersona(t, dir, "persona-facet", personaManifestJSON("persona-facet"), personaFilesOK())
	writePersona(t, dir, "noidle", strings.Replace(personaManifestJSON("noidle"), `"idle"`, `"thinking"`, 1), personaFilesOK())
	writePersona(t, dir, "escape", strings.Replace(personaManifestJSON("escape"), "avatar/128.png", "../doudou/avatar/128.png", 1), personaFilesOK())
	writePersona(t, dir, "future", strings.Replace(personaManifestJSON("future"), `"format":1`, `"format":2`, 1), personaFilesOK())

	srv := httptest.NewServer(h.routes())
	defer srv.Close()
	list := getPersonas(t, srv, h.cfg.Token)
	if len(list) != 1 || list[0].ID != "doudou" {
		t.Fatalf("list = %+v, want only doudou", list)
	}
	p := list[0]
	if len(p.Rev) != 16 || p.Bytes != int64(len("png-a")+len("png-i")+len("png-w")+len(personaManifestJSON("doudou"))) {
		t.Fatalf("rev %q bytes %d", p.Rev, p.Bytes)
	}
	// The manifest goes out as written (the app reads the colors and frame rates the hub does not check).
	var m map[string]any
	if err := json.Unmarshal(p.Manifest, &m); err != nil || m["colors"] == nil {
		t.Fatalf("manifest %s: %v", p.Manifest, err)
	}

	// Any byte of the package changes the rev; the same bytes give the same one.
	if again := getPersonas(t, srv, h.cfg.Token); again[0].Rev != p.Rev {
		t.Fatalf("rev moved without a change: %s → %s", p.Rev, again[0].Rev)
	}
	if err := os.WriteFile(filepath.Join(dir, "doudou", "dot", "idle.png"), []byte("png-I"), 0o644); err != nil {
		t.Fatal(err)
	}
	if after := getPersonas(t, srv, h.cfg.Token); after[0].Rev == p.Rev {
		t.Fatal("rev did not change with a file")
	}
}

func TestPersonaFilesStayInTheirPackage(t *testing.T) {
	h := newTestHub(t)
	dir := h.personasDir()
	files := personaFilesOK()
	files["preview.png"] = "png-p"
	files[".secret"] = "no"
	writePersona(t, dir, "doudou", personaManifestJSON("doudou"), files)
	if err := os.WriteFile(filepath.Join(dir, "outside.txt"), []byte("no"), 0o644); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(h.routes())
	defer srv.Close()

	for p, want := range map[string]string{"doudou/dot/idle.png": "png-i", "doudou/preview.png": "png-p", "doudou/persona.json": `"format":1`} {
		code, body := getPersonaFile(t, srv, h.cfg.Token, p)
		if code != 200 || !strings.Contains(body, want) {
			t.Fatalf("%s: %d %q", p, code, body)
		}
	}
	for _, p := range []string{"doudou/.secret", "doudou/../outside.txt", "doudou/%2e%2e/outside.txt", "doudou/nope.png", "nobody/dot/idle.png"} {
		if code, body := getPersonaFile(t, srv, h.cfg.Token, p); code == 200 {
			t.Fatalf("%s served: %q", p, body)
		}
	}
	// Without the token: nothing.
	if code, _ := getPersonaFile(t, srv, "", "doudou/dot/idle.png"); code != http.StatusUnauthorized {
		t.Fatalf("no token: %d", code)
	}
}

func TestPersonasNoDirectory(t *testing.T) {
	h := newTestHub(t)
	srv := httptest.NewServer(h.routes())
	defer srv.Close()
	if list := getPersonas(t, srv, h.cfg.Token); list == nil || len(list) != 0 {
		t.Fatalf("list = %#v, want an empty list", list)
	}
}

// The watch's Milk Frog (#6272) sits in the built-in package without touching it: the package still checks out within
// its limits, and a phone's rev covers only its tier's files, so the clips never reach a phone's download.
func TestBuiltinMilkFrogCarriesTheWatchPet(t *testing.T) {
	h := newTestHub(t)
	h.builtinLooks = builtinPersonaFS()
	p, err := h.personaPackage("nwverse")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := p.files["pet/pet.json"]; !ok {
		t.Fatal("no pet/pet.json in the package")
	}
	var pet int64
	for f, size := range p.files {
		if strings.HasPrefix(f, "pet/") {
			pet += size
		}
	}
	info, err := h.personaInfo(p)
	if err != nil {
		t.Fatal(err)
	}
	if info.Bytes > p.total-pet {
		t.Fatalf("a phone's download includes the watch's files: %d of %d, %d of them the pet's", info.Bytes, p.total, pet)
	}
}
