package transcript

import "github.com/chyroc/jarvis/server/internal/proto"

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
