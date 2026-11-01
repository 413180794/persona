package tunnel

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// The upload port takes a new jarvisd for the node, what the hub's 重新部署 sends over ssh otherwise (hub/deploy.go):
// one JSON line {"size","sha256"} of the binary, the binary gzipped, then the node answers one JSON line
// {"ok"} or {"error"}. On ok the node has replaced its own executable and exits, and its supervisor (launchd,
// systemd: scripts/node-launchd.sh) starts the new one.

// maxBinary bounds what an upload may write.
const maxBinary = 512 << 20

type uploadHeader struct {
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

type uploadReply struct {
	OK    bool   `json:"ok,omitempty"`
	Error string `json:"error,omitempty"`
}

// ServeUpload accepts uploads on ln until it closes, installing each over exe. done runs after a successful one
// (the reply already sent): the node should exit so the new binary runs.
func ServeUpload(ln net.Listener, exe string, done func()) { serveUpload(ln, exe, done, uploadIdle) }

func serveUpload(ln net.Listener, exe string, done func(), idle time.Duration) {
	var mu sync.Mutex // one at a time: two would race on the same file
	for {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		go func() {
			defer c.Close()
			// No total deadline (tens of MB over a slow relay take a while), but one that moves with every read: a hub
			// gone mid-upload (redeployed, restarted) must not hold the upload for long, the next one is turned away.
			if !mu.TryLock() {
				writeReply(c, errors.New("another upload is in progress"))
				return
			}
			defer mu.Unlock()
			err := install(bufio.NewReader(idleReader{c, idle}), exe)
			_ = c.SetWriteDeadline(time.Now().Add(30 * time.Second))
			writeReply(c, err)
			if err == nil && done != nil {
				if cw, ok := c.(interface{ CloseWrite() error }); ok {
					_ = cw.CloseWrite()
				}
				time.Sleep(time.Second) // let the reply leave before the process does
				done()
			}
		}()
	}
}

// uploadIdle: how long an upload may go without a byte arriving.
const uploadIdle = 60 * time.Second

// idleReader moves the connection's read deadline before every read.
type idleReader struct {
	c    net.Conn
	idle time.Duration
}

func (r idleReader) Read(p []byte) (int, error) {
	_ = r.c.SetReadDeadline(time.Now().Add(r.idle))
	return r.c.Read(p)
}

func writeReply(c net.Conn, err error) {
	reply := uploadReply{OK: err == nil}
	if err != nil {
		reply.Error = err.Error()
	}
	b, _ := json.Marshal(reply)
	_, _ = c.Write(append(b, '\n'))
}

func install(r *bufio.Reader, exe string) error {
	line, err := r.ReadBytes('\n')
	if err != nil {
		return fmt.Errorf("read header: %w", err)
	}
	var h uploadHeader
	if err := json.Unmarshal(line, &h); err != nil {
		return fmt.Errorf("header: %w", err)
	}
	if h.Size <= 0 || h.Size > maxBinary {
		return fmt.Errorf("size %d out of range", h.Size)
	}
	want, err := hex.DecodeString(h.SHA256)
	if err != nil || len(want) != sha256.Size {
		return errors.New("bad sha256")
	}
	zr, err := gzip.NewReader(r)
	if err != nil {
		return fmt.Errorf("gzip: %w", err)
	}
	zr.Multistream(false) // stop at the end of the binary: the connection stays open for the answer
	tmp := exe + ".new"
	_ = os.Remove(tmp)
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o700)
	if err != nil {
		return err
	}
	defer os.Remove(tmp) // a no-op once renamed
	sum := sha256.New()
	n, err := io.Copy(io.MultiWriter(f, sum), io.LimitReader(zr, h.Size+1))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	switch {
	case err != nil:
		return fmt.Errorf("receive: %w", err)
	case n != h.Size:
		return fmt.Errorf("got %d bytes, want %d", n, h.Size)
	case !bytes.Equal(sum.Sum(nil), want):
		return errors.New("sha256 mismatch")
	}
	if err := os.Chmod(tmp, 0o755); err != nil {
		return err
	}
	// It must run here (right OS and architecture, not truncated) before it replaces the working one.
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if out, err := exec.CommandContext(ctx, tmp, "version").CombinedOutput(); err != nil {
		return fmt.Errorf("the new binary does not run here: %v %s", err, strings.TrimSpace(string(out)))
	}
	return os.Rename(tmp, exe)
}

// Upload sends the binary at path over c (a connection to UploadPort) and waits for the node's answer.
func Upload(ctx context.Context, c net.Conn, path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	sum := sha256.New()
	size, err := io.Copy(sum, f)
	if err != nil {
		return err
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return err
	}
	stop := context.AfterFunc(ctx, func() { _ = c.SetDeadline(time.Now()) })
	defer stop()
	head, _ := json.Marshal(uploadHeader{Size: size, SHA256: hex.EncodeToString(sum.Sum(nil))})
	w := bufio.NewWriter(c)
	if _, err := w.Write(append(head, '\n')); err != nil {
		return err
	}
	zw, _ := gzip.NewWriterLevel(w, gzip.BestSpeed)
	if _, err := io.Copy(zw, f); err != nil {
		return fmt.Errorf("send: %w", err)
	}
	if err := zw.Close(); err != nil {
		return fmt.Errorf("send: %w", err)
	}
	if err := w.Flush(); err != nil {
		return fmt.Errorf("send: %w", err)
	}
	line, err := bufio.NewReader(c).ReadBytes('\n')
	if err != nil {
		return fmt.Errorf("no answer from the node: %w", err)
	}
	var reply uploadReply
	if err := json.Unmarshal(line, &reply); err != nil {
		return fmt.Errorf("answer: %w", err)
	}
	if !reply.OK {
		return errors.New(reply.Error)
	}
	return nil
}

// Executable is the file a running jarvisd was started from, symlinks resolved: what an upload replaces.
func Executable() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(exe)
}
