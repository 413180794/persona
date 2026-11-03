package node

import (
	"fmt"
	"io"
	"mime"
	"os"
	"path/filepath"
	"strings"

	"github.com/nwverse/persona/server/internal/proto"
)

func (n *Node) readFileChunk(p proto.FileChunkRequest) (*proto.FileChunk, error) {
	if p.Offset < 0 || p.Length < 0 || p.Length > proto.FileDownloadChunkBytes {
		return nil, fmt.Errorf("invalid file chunk range")
	}
	path := n.resolvePath(p.Path, p.Cwd)
	if path == "" {
		return nil, fmt.Errorf("path cannot be resolved")
	}
	if inICloud(path) {
		return nil, fmt.Errorf("iCloud Drive files are not readable remotely")
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return nil, explainDenied(path, n.home, err)
	}
	if inICloud(resolved) {
		return nil, fmt.Errorf("iCloud Drive files are not readable remotely")
	}
	before, err := os.Stat(resolved)
	if err != nil {
		return nil, explainDenied(path, n.home, err)
	}
	if !before.Mode().IsRegular() {
		return nil, fmt.Errorf("only regular files can be downloaded")
	}
	f, err := os.Open(resolved)
	if err != nil {
		return nil, explainDenied(path, n.home, err)
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !st.Mode().IsRegular() {
		return nil, fmt.Errorf("only regular files can be downloaded")
	}
	revision := fmt.Sprintf("%x-%x", st.Size(), st.ModTime().UnixNano())
	if p.Revision != "" && p.Revision != revision {
		return nil, fmt.Errorf("file changed during download; reopen it to try again")
	}
	if p.Offset > st.Size() {
		return nil, fmt.Errorf("offset is outside the file")
	}
	out := &proto.FileChunk{Path: path, Name: filepath.Base(path), Size: st.Size(),
		Mime: mime.TypeByExtension(strings.ToLower(filepath.Ext(path))), Modified: st.ModTime(),
		Revision: revision, Offset: p.Offset}
	if p.Length == 0 {
		return out, nil
	}
	out.Data = make([]byte, min(int64(p.Length), st.Size()-p.Offset))
	nr, err := f.ReadAt(out.Data, p.Offset)
	if err != nil && err != io.EOF {
		return nil, err
	}
	if nr != len(out.Data) {
		return nil, io.ErrUnexpectedEOF
	}
	after, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if after.Size() != st.Size() || !after.ModTime().Equal(st.ModTime()) {
		return nil, fmt.Errorf("file changed during download; reopen it to try again")
	}
	return out, nil
}
