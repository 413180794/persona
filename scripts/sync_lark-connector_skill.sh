#!/bin/bash
# Copies lark-connector's agent skill into skills/lark-connector: the repo's copy, which goes into ~/.jarvis/skills on the machine
# that runs the assistant (docs/hub-update.md「skill 从二进制里拿出来」), never into jarvisd.
#   ./scripts/sync_lark-connector_skill.sh <lark-connector checkout>
# Only SKILL.md, references/ and scripts/ol.sh: the evals and the Node scripts stay out (the hub has no Node).
set -euo pipefail
src=${1:?usage: sync_lark-connector_skill.sh <lark-connector checkout>}
cd "$(dirname "$0")/.."
skill="$src/.agents/skills/lark-connector"
[ -f "$skill/SKILL.md" ] && [ -f "$skill/scripts/ol.sh" ] || { echo "no .agents/skills/lark-connector in $src" >&2; exit 1; }
dst=skills/lark-connector
rm -rf "$dst"
mkdir -p "$dst/references" "$dst/scripts"
cp "$skill/SKILL.md" "$dst/"
cp "$skill"/references/*.md "$dst/references/"
cp "$skill/scripts/ol.sh" "$dst/scripts/"
commit=$(git -C "$src" log -1 --format='%H')
day=$(git -C "$src" log -1 --format='%cs')
sed -i.bak -E "s/\`[0-9a-f]{40}\`（[0-9-]+）/\`${commit}\`（${day}）/" skills/SOURCE.md
rm -f skills/SOURCE.md.bak
echo "RESULT: lark-connector skill synced to $commit ($day); review git diff $dst and node/larkgate.go's default rules"
