Answer another agent's question for the user, in their place, when they said what to answer or told you to decide. Only a question can be answered this way: a tool permission, a plan and a terminal dialog are theirs to decide in the app, and the assistant's own question is never answered by itself. Every question must be answered by its own wording, with the option labels it offers; free text is not accepted (the user answers that in the app, or you use agent_send). A multi-select takes an array of labels.

```json
{
  "type": "object",
  "required": ["machine", "session_id", "interaction_id", "answers"],
  "properties": {
    "machine": {
      "type": "string",
      "description": "the machine's id or name, as agents_list shows it"
    },
    "session_id": {
      "type": "string",
      "description": "the agent's session id, or its first 8 characters"
    },
    "interaction_id": {
      "type": "string",
      "description": "the waiting interaction's id, from agents_list"
    },
    "answers": {
      "type": "object",
      "description": "each question's own wording → the option label to pick; a multi-select gives an array of labels",
      "additionalProperties": {
        "oneOf": [
          { "type": "string" },
          { "type": "array", "items": { "type": "string" } }
        ]
      }
    }
  }
}
```
