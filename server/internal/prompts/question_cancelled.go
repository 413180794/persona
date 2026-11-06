package prompts

// RenderQuestionCancelled reports a closed question without inventing an answer.
func RenderQuestionCancelled() string { return render("agents/question_cancelled", nil) }
