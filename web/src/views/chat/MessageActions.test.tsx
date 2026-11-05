import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import { ChatActionsContext, type ChatActions } from "./chat-actions";
import { MessageActions, moreActions } from "./MessageActions";

const base: ChatActions = { draftKey: "m/s", canCompose: true, images: () => [] };

function render(actions: ChatActions, itemId?: string) {
  return renderToStaticMarkup(
    <ChatActionsContext.Provider value={actions}>
      <MessageActions text="回复" itemId={itemId}>
        <p>回复</p>
      </MessageActions>
    </ChatActionsContext.Provider>,
  );
}

// 分支到新聊天 (#2137): a reply's bar ends in ⋯ only when the session can fork.
describe("MessageActions ⋯", () => {
  it("shows ⋯ on a reply of a session that can fork", () => {
    expect(render({ ...base, fork: () => {} }, "u1#0")).toContain('aria-label="更多"');
  });
  it("has no ⋯ when the session cannot fork, or the message is not a reply", () => {
    expect(render(base, "u1#0")).not.toContain('aria-label="更多"');
    expect(render({ ...base, fork: () => {} })).not.toContain('aria-label="更多"');
  });
});

describe("moreActions", () => {
  it("branches off at the reply it is on", () => {
    const got: string[] = [];
    const [fork] = moreActions("u1#0", (id) => got.push(id));
    expect(fork?.label).toBe("分支到新聊天");
    fork?.onSelect();
    expect(got).toEqual(["u1#0"]);
  });
  it("offers nothing for a line the node cannot find", () => {
    expect(moreActions("line12#0", () => {})).toEqual([]);
    expect(moreActions("u1", () => {})).toEqual([]);
    expect(moreActions(undefined, () => {})).toEqual([]);
  });
});
