# Launchers written as aliases, in the shapes found in real ~/.zshrc files (placeholder hosts, fake keys), for
# launchers_test.go: what `personad` lists from the alias table (launcherScript / parseAliasLaunchers).
alias kcc="CLAUDE_CODE_NO_FLICKER=1 ANTHROPIC_BASE_URL=https://gw-a.example.com/anthropic ANTHROPIC_AUTH_TOKEN=sk-fake-aaaaaaaaaaaaaaaaaaaa ANTHROPIC_MODEL=model-a[1m] ANTHROPIC_DEFAULT_OPUS_MODEL=model-a[1m] claude --teammate-mode in-process --dangerously-skip-permissions"
alias dmcc="CLAUDE_CODE_NO_FLICKER=1 ANTHROPIC_BASE_URL=https://gw-b.example.com ANTHROPIC_AUTH_TOKEN=-fakeTokenWithALeadingDash000000000000 API_TIMEOUT_MS=3000000 claude --dangerously-skip-permissions"
alias spaced='ANTHROPIC_BASE_URL="https://gw-c.example.com" ANTHROPIC_MODEL="model with space" claude'
alias mycc="ANTHROPIC_MODEL=model-default claude --teammate-mode in-process --dangerously-skip-permissions"
alias ncc="claude --teammate-mode in-process --dangerously-skip-permissions"
alias envcc="env -u ANTHROPIC_API_KEY ANTHROPIC_BASE_URL=https://gw-d.example.com claude"
alias pathcc="ANTHROPIC_BASE_URL=https://gw-e.example.com /opt/homebrew/bin/claude --model flag-model"
alias expcc="export ANTHROPIC_BASE_URL=https://gw-f.example.com; claude --model=eq-model"
alias ncodex="codex --yolo"
alias claude="claude --dangerously-skip-permissions"
alias ll="ls -l"
alias gst="git status"
