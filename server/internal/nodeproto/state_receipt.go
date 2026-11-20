package nodeproto

const (
	MethodStateReceipt    = "state.receipt"
	MethodStatePutReceipt = "state.put.receipt"
	CodeStatePredecessor  = "state_predecessor_changed"
	CodeStatePaused       = "state_admission_paused"
)

type StateRevision struct {
	Revision string `json:"revision"`
}
