package prompts

// ViewErrors is the <persona-view-errors> block a message to the assistant carries after it (assistant/view_errors):
// the problems of its replies' rich views that failed their check and were not reported yet. Each error is already
// cut short and stripped of what the view said (assistant/context_view_errors.go).
type ViewErrors struct {
	Replies []ViewErrorReply
	Later   bool // more replies have problems, left for the next message
}

// ViewErrorReply is one reply's problems, all of them; At is when it was written ("11-05 09:30").
type ViewErrorReply struct {
	At     string
	Errors []string
}

func RenderViewErrors(v ViewErrors) string { return render("assistant/view_errors", v) }
