package hub

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
)

func (h *Hub) androidAppRoutes(api *http.ServeMux) {
	api.HandleFunc("GET /api/v1/app/android/latest", h.androidAppLatest)
	api.HandleFunc("GET /api/v1/app/android/apk", h.androidAppAPK)
}

func (h *Hub) androidAppManifest(w http.ResponseWriter) ([]byte, string, bool) {
	dir := filepath.Join(h.cfg.dir, "apps", "android")
	b, err := os.ReadFile(filepath.Join(dir, "latest.json"))
	if err != nil {
		androidAppError(w, err)
		return nil, "", false
	}
	return b, dir, true
}

func (h *Hub) androidAppLatest(w http.ResponseWriter, r *http.Request) {
	b, _, ok := h.androidAppManifest(w)
	if !ok {
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_, _ = w.Write(b)
}

func (h *Hub) androidAppAPK(w http.ResponseWriter, r *http.Request) {
	b, dir, ok := h.androidAppManifest(w)
	if !ok {
		return
	}
	// Content-addressed files let publication replace only the manifest atomically.
	var release struct {
		SHA256 string `json:"sha256"`
	}
	err := json.Unmarshal(b, &release)
	digest, decodeErr := hex.DecodeString(release.SHA256)
	if err != nil || decodeErr != nil || len(digest) != 32 {
		writeErr(w, http.StatusInternalServerError, errors.New("Android 版本清单无效"))
		return
	}
	f, err := os.Open(filepath.Join(dir, release.SHA256+".apk"))
	if err != nil {
		androidAppError(w, err)
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		writeErr(w, http.StatusInternalServerError, errors.New("无法读取 Android 安装包"))
		return
	}
	w.Header().Set("Content-Type", "application/vnd.android.package-archive")
	w.Header().Set("Content-Disposition", `attachment; filename="jarvis.apk"`)
	w.Header().Set("ETag", `"`+release.SHA256+`"`)
	http.ServeContent(w, r, "jarvis.apk", info.ModTime(), f)
}

func androidAppError(w http.ResponseWriter, err error) {
	if os.IsNotExist(err) {
		writeErrCode(w, http.StatusNotFound, "not_found", errors.New("尚未发布 Android 安装包"))
		return
	}
	writeErr(w, http.StatusInternalServerError, errors.New("无法读取 Android 发布文件"))
}
