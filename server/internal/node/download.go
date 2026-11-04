package node

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/nwverse/persona/server/internal/proto"
)

func (n *Node) readFileChunk(p proto.FileChunkRequest) (*proto.FileChunk, error) {
	if p.Offset < 0 || p.Length < 0 || p.Length > proto.FileDownloadChunkBytes {
		return nil, fmt.Errorf("invalid file chunk range")
	}
	path, f, st, err := n.openRegular(p.Path, p.Cwd)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	revision := fileRevision(st)
	if p.Revision != "" && p.Revision != revision {
		return nil, fmt.Errorf("file changed during download; reopen it to try again")
	}
	if p.Offset > st.Size() {
		return nil, fmt.Errorf("offset is outside the file")
	}
	out := &proto.FileChunk{Path: path, Name: filepath.Base(path), Size: st.Size(),
		Modified: st.ModTime(), Revision: revision, Offset: p.Offset}
	if p.Length == 0 { // the first call, for what the hub sends as Content-Type: sniff when the name says nothing
		out.Mime = fileMime(path, readHead(f))
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

// openRegular opens a regular file a client asked for by its conversation path (download, thumbnail), following
// links but never into iCloud Drive. path is the resolved path before links.
func (n *Node) openRegular(asked, cwd string) (path string, f *os.File, st os.FileInfo, err error) {
	path = n.resolvePath(asked, cwd)
	if path == "" {
		return "", nil, nil, fmt.Errorf("path cannot be resolved")
	}
	if inICloud(path) {
		return "", nil, nil, fmt.Errorf("iCloud Drive files are not readable remotely")
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", nil, nil, explainDenied(path, n.home, err)
	}
	if inICloud(resolved) {
		return "", nil, nil, fmt.Errorf("iCloud Drive files are not readable remotely")
	}
	before, err := os.Stat(resolved)
	if err != nil {
		return "", nil, nil, explainDenied(path, n.home, err)
	}
	if !before.Mode().IsRegular() {
		return "", nil, nil, fmt.Errorf("only regular files can be downloaded")
	}
	if f, err = os.Open(resolved); err != nil {
		return "", nil, nil, explainDenied(path, n.home, err)
	}
	if st, err = f.Stat(); err != nil {
		f.Close()
		return "", nil, nil, err
	}
	if !st.Mode().IsRegular() {
		f.Close()
		return "", nil, nil, fmt.Errorf("only regular files can be downloaded")
	}
	return path, f, st, nil
}
