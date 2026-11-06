Start a new Claude Code agent in the terminal engine (herdr or Orca) of one of the user's computers, in a project directory, with a first prompt. Use for real development work instead of doing it here. Returns the new session id. When the launcher failed its last health check on that machine, the agent starts on a machine where it works (a directory under the home moves to that home) and the answer says so.

When the work needs a branch and checkout of its own (work in parallel with what is already open in that repository), give `worktree.branch`: Persona makes the worktree through the machine's engine first (herdr's or Orca's own, so it is listed there and in the app, under the workspace `cwd` is in) and starts the agent in it. Do not tell the agent to run `git worktree add` itself: a worktree made that way is invisible to Orca and to the app. An existing local branch is checked out; a new one starts at the HEAD of the workspace `cwd` is in. A branch already checked out elsewhere is refused.

```json
{
  "type": "object",
  "required": ["machine", "cwd", "prompt"],
  "properties": {
    "machine": {
      "type": "string"
    },
    "cwd": {
      "type": "string",
      "description": "absolute path or ~/…; with worktree, a directory of the repository to branch from"
    },
    "prompt": {
      "type": "string"
    },
    "launcher": {
      "type": "string",
      "description": "shell command that starts claude (default: the machine's default)"
    },
    "worktree": {
      "type": "object",
      "description": "start the agent in a new worktree of cwd's repository, made by the machine's engine",
      "required": ["branch"],
      "properties": {
        "branch": {
          "type": "string",
          "description": "the branch to check out (made from the workspace's HEAD when it does not exist)"
        }
      }
    }
  }
}
```
