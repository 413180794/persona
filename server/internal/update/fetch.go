package update

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Published is what the publisher put up, as a hub sees it. Nothing is signed (the user's call, docs/operations/hub-update.md
// "Security"): the sha256s guard against a broken download over HTTPS, not against the site itself being replaced.
type Published struct {
	Manifest *Manifest
}

// Check reads latest.json from base (the publisher's dl/).
func Check(ctx context.Context, client *http.Client, base string) (*Published, error) {
	if base == "" {
		return nil, errors.New("this personad has nowhere to look for releases")
	}
	data, err := get(ctx, client, strings.TrimSuffix(base, "/")+"/latest.json", 1<<20)
	if err != nil {
		return nil, err
	}
	m, err := ParseManifest(data)
	if err != nil {
		return nil, err
	}
	return &Published{Manifest: m}, nil
}

var errNotFound = errors.New("not found")

func get(ctx context.Context, client *http.Client, url string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, errNotFound
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: HTTP %d", url, resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, limit))
}

// Download fetches f from base into dir and unpacks it to dir/<name without .gz>: the .gz must match the manifest's
// size and sha256, what it unpacks to its binary_sha256. A broken download is tried again (3 times in
// all); anything that does not match is deleted and nothing is left half-written.
func Download(ctx context.Context, client *http.Client, base string, f File, dir string) (string, error) {
	if f.Name == "" || filepath.Base(f.Name) != f.Name || strings.ContainsAny(f.Name, `/\`) {
		return "", errors.New("invalid artifact name")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	gz := filepath.Join(dir, f.Name)
	var err error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-time.After(time.Duration(attempt) * 2 * time.Second):
			}
		}
		for _, name := range artifactNames(f.Name) {
			err = fetchFile(ctx, client, strings.TrimSuffix(base, "/")+"/"+name, gz, f)
			if !errors.Is(err, errNotFound) {
				break
			}
		}
		if err == nil {
			break
		}
		if errors.Is(err, errMismatch) || errors.Is(err, errNotFound) {
			return "", err // the file served is not the one in the manifest: trying again will not help
		}
	}
	if err != nil {
		return "", err
	}
	bin := filepath.Join(dir, strings.TrimSuffix(f.Name, ".gz"))
	if err := unpack(gz, bin, f.BinarySHA256); err != nil {
		return "", err
	}
	return bin, nil
}

var errMismatch = errors.New("the download does not match the release's manifest")

func fetchFile(ctx context.Context, client *http.Client, url, path string, f File) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return errNotFound
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s: HTTP %d", url, resp.StatusCode)
	}
	tmp := path + ".part"
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	defer os.Remove(tmp)
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(out, h), io.LimitReader(resp.Body, f.Size+1))
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err // cut off: the next attempt starts again
	}
	if n != f.Size || hex.EncodeToString(h.Sum(nil)) != f.SHA256 {
		if n < f.Size {
			return fmt.Errorf("%s: got %d of %d bytes", url, n, f.Size)
		}
		return fmt.Errorf("%w: %s", errMismatch, f.Name)
	}
	return os.Rename(tmp, path)
}

func unpack(gz, bin, want string) error {
	in, err := os.Open(gz)
	if err != nil {
		return err
	}
	defer in.Close()
	zr, err := gzip.NewReader(in)
	if err != nil {
		return err
	}
	tmp := bin + ".part"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
	if err != nil {
		return err
	}
	defer os.Remove(tmp)
	h := sha256.New()
	_, err = io.Copy(io.MultiWriter(out, h), zr)
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err == nil && hex.EncodeToString(h.Sum(nil)) != want {
		err = fmt.Errorf("%w: %s unpacked", errMismatch, filepath.Base(gz))
	}
	if err != nil {
		return err
	}
	return os.Rename(tmp, bin)
}

// CheckBinary runs the new binary's `version -v`: it must say the release's version, the release channel and its
// build number, or it is not the release's binary for this platform (or does not run here).
func CheckBinary(ctx context.Context, bin string, m *Manifest) error {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, bin, "version", "-v").Output()
	if err != nil {
		return fmt.Errorf("the new personad does not run here: %w", err)
	}
	want := fmt.Sprintf("%s\nchannel %s, build %d", m.Version, Release, m.Build)
	if got := strings.TrimSpace(string(bytes.TrimSpace(out))); got != want {
		return fmt.Errorf("the new personad says %q, not %q", got, want)
	}
	return nil
}
