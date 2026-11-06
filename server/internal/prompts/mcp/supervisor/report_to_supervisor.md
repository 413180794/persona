Report to the Persona assistant that handed you this task and reviews it (your supervisor), not to the user: a decision you need from it (question), that you are stuck (blocked), that you finished with the evidence for each acceptance criterion (done), or where you are in a long task (progress). It reads it within minutes, and answers or redirects you by message. Only for a task Persona supervises; elsewhere nobody receives it and it says so. Don't use it for every small step, and keep asking the user what only the user can decide the usual way.

```json
{
  "type": "object",
  "required": ["kind", "text"],
  "properties": {
    "kind": {
      "type": "string",
      "enum": ["progress", "question", "blocked", "done"]
    },
    "text": {
      "type": "string",
      "description": "what you report, in a few lines: the question with its options, what blocks you, what you did"
    },
    "evidence": {
      "type": "array",
      "maxItems": 8,
      "items": { "type": "string" },
      "description": "links, command output, file paths that show it (a PR, CI, a screenshot)"
    },
    "step": {
      "type": "integer",
      "description": "the step you are on (progress)"
    },
    "steps": {
      "type": "integer",
      "description": "how many steps there are in all"
    }
  }
}
```
