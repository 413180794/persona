import { describe, expect, it } from "vitest";
import { renderToStaticMarkup } from "react-dom/server";
import { WorkspaceCard } from "./WorkspaceBoard";
import type { BoardCard } from "../lib/workspace-board";
const card: BoardCard = { id: "card", machine_id: "computer", workspace_id: "space", title: "Project", cwd: "/projects/demo", source: "directory", column_id: "review", pinned: true, archived: false };
const draw = (offline = false, disabled = false) => renderToStaticMarkup(<WorkspaceCard card={card} machine={{ id: "computer", name: "Computer", online: !offline }} sessions={[]} disabled={disabled} onOpen={() => {}} onMenu={() => {}} />);
describe("WorkspaceCard", () => {
  it("renders a genuine workspace card without any running Agent", () => { const html = draw(); expect(html).toContain("Project"); expect(html).toContain("/projects/demo"); expect(html).toContain("暂无 Agent"); expect(html).toContain('data-card-id="card"'); expect(html).toContain('draggable="true"'); });
  it("keeps offline, agent-free cards visible and makes readonly cards non-draggable", () => { const html = draw(true, true); expect(html).toContain("Project"); expect(html).toContain("机器离线"); expect(html).toContain('draggable="false"'); expect(html).toContain("disabled"); expect(card.column_id).toBe("review"); });
});
