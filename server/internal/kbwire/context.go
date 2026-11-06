package kbwire

// ChatContext and ChatProject are what the knowledge base knows about a chat and its projects: the manager answers
// kb.context with them, and the prompts render them (prompts' shared/kb_blocks.md).
type ChatProject struct {
	Name, Summary string
}

type ChatContext struct {
	ChatID, ChatName string
	Summary          string // "" leaves the chat's block out
	Projects         []ChatProject
}
