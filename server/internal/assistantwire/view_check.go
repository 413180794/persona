package assistantwire

const MethodViewCheck = "assistant.view.check"

type ViewCheckRequest struct {
	SessionID string `json:"session_id"`
	Message   string `json:"message"`
}

type ViewCheckResponse struct {
	Reason string `json:"reason,omitempty"`
}
