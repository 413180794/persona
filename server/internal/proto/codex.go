package proto

// CodexConfig is the non-secret inventory remembered by the hub. Live owners and connection generations are never persisted.
type CodexConfig struct {
	BlockedIDs []string          `json:"blocked_ids,omitempty"`
	Discovery  *bool             `json:"discovery,omitempty"`
	Homes      []string          `json:"homes,omitempty"`
	LegacyHome string            `json:"legacy_home,omitempty"`
	Servers    []CodexServer     `json:"servers,omitempty"`
	Joined     map[string]string `json:"joined,omitempty"` // public session id -> stable instance id
}

type CodexServer struct {
	ID       string `json:"id"`
	Home     string `json:"home"`
	Endpoint string `json:"endpoint"`
	Name     string `json:"name,omitempty"`
}

type SessionCapabilities struct {
	Read      bool `json:"read"`
	Send      bool `json:"send"`
	Interrupt bool `json:"interrupt"`
	Approve   bool `json:"approve"`
	Terminal  bool `json:"terminal"`
	Attach    bool `json:"attach"`
}
