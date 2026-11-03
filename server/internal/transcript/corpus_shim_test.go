package transcript

import "github.com/nwverse/persona/server/internal/proto"

// shimFull gives the item whole (pages carry light copies: long text cut, images without data).
func shimFull(f *File, it proto.Item) proto.Item {
	if it.FullSize > 0 || (it.Image != nil && it.Image.Lazy) {
		if full, err := f.Full(it.ID); err == nil {
			return full
		}
	}
	return it
}

func shimFirstPage(f *File) int {
	items, _, _ := f.PageTurns(0, 2, 2000)
	return len(items)
}

// shimWhole makes cold opens read files whole (the comparison is of what the parser makes of every line; window.go
// reads big files by their end). It gives back the setting to restore.
func shimWhole() func() {
	w := windowBytes
	windowBytes = 1 << 50
	return func() { windowBytes = w }
}
