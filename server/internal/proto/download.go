package proto

import "time"

const FileDownloadChunkBytes = 1 << 20

type FileChunkRequest struct {
	Path     string `json:"path"`
	Cwd      string `json:"cwd"`
	Offset   int64  `json:"offset"`
	Length   int    `json:"length"`
	Revision string `json:"revision,omitempty"`
}

type FileChunk struct {
	Path     string    `json:"path"`
	Name     string    `json:"name"`
	Size     int64     `json:"size"`
	Mime     string    `json:"mime"`
	Modified time.Time `json:"modified"`
	Revision string    `json:"revision"`
	Offset   int64     `json:"offset"`
	Data     []byte    `json:"data,omitempty"`
}
