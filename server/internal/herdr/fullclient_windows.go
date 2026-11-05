//go:build windows

package herdr

// FullClient does not exist on Windows (herdr's TUI is a Unix program); StartFull says so.
type FullClient struct{}

func (c *Client) StartFull(FullOptions, func([]byte)) (*FullClient, error) {
	return nil, ErrFullUnsupported
}

func (c *FullClient) Write([]byte) error          { return ErrFullUnsupported }
func (c *FullClient) Resize(cols, rows int) error { return ErrFullUnsupported }
func (c *FullClient) Done() <-chan struct{} {
	ch := make(chan struct{})
	close(ch)
	return ch
}
func (c *FullClient) Err() error { return ErrFullUnsupported }
func (c *FullClient) Close()     {}
