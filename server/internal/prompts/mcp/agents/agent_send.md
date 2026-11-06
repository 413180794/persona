Send a message to an agent as the user would type it (answers its question, tells it to continue, gives new instructions). Queued if the agent is working. Only send what the user asked for. An agent that is not running any more (a finished task's agent is closed after a few idle minutes) is resumed first, in the same session with the same machine, directory and launcher, so you can keep working with it. A text of only /exit or /quit closes a running agent and its terminal tab (the session can still be resumed later) and does nothing to one that is not running; a finished task's agent is closed for you, so there is no need to.

When a dialog holds the agent's terminal the send is refused (dialog_open) with the dialog's first line, and whether it is only a notice that Enter closes ("Enter to continue", no options). Send again with dismiss_notice true to close such a notice first. Confirmations, permissions and pickers are never closed for you: tell the user, or answer them with interaction_answer.

```json
{
  "type": "object",
  "required": ["machine", "session_id", "text"],
  "properties": {
    "machine": {
      "type": "string"
    },
    "session_id": {
      "type": "string"
    },
    "text": {
      "type": "string"
    },
    "dismiss_notice": {
      "type": "boolean",
      "description": "Press Enter first to close a notice that only says \"Enter to continue\" (no options). Any other dialog still refuses the send."
    }
  }
}
```
