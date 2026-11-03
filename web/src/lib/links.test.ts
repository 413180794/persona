import { describe, expect, it, vi } from "vitest";

// router.ts listens to popstate at load (no window in node); links.ts only needs navigate from it.
vi.mock("./router", () => ({ navigate: vi.fn() }));
vi.mock("./api", () => ({ api: vi.fn(), enc: encodeURIComponent, getToken: () => "tok", ApiError: class extends Error {} }));

import { parseUserText } from "./turns";
import {
  activeComposer,
  addContext,
  contextXML,
  docKind,
  editMessage,
  getContexts,
  linkHref,
  linkRoute,
  newContext,
  opensInApp,
  registerComposer,
  removeContext,
  usableComposer,
  whenComposer,
  withContexts,
  WHOLE,
} from "./links";
import { BR, docErrorFromBody, orderedNumbers, prepareMarkdown, readDoc, type DocNode, type Get } from "../views/link/feishu-doc";

describe("where a link opens", () => {
  it("reads Feishu documents in the panel, like the phone's FeishuLinks.isDoc", () => {
    for (const u of [
      "https://example.feishu.cn/docx/AbC123",
      "https://example.feishu.cn/wiki/AbC123?from=x",
      "https://example.larkoffice.com/docs/AbC",
      "https://sub.example.larksuite.com/sheets/AbC",
      "https://example.feishu.cn/mindnotes/AbC",
      "https://example.feishu.cn/slides/AbC",
      "https://example.feishu.cn/file/AbC",
      "https://feishu.cn/docx/AbC",
    ]) {
      expect(linkRoute(u).kind, u).toBe("doc");
      expect(opensInApp(u), u).toBe(true);
    }
    expect(linkRoute("https://example.feishu.cn/docx/AbC")).toEqual({ kind: "doc", doc: "doc" });
    expect(docKind("https://example.feishu.cn/wiki/AbC")).toBe("wiki");
    expect(docKind("https://example.feishu.cn/sheets/AbC")).toBe("sheet");
  });

  it("sends a Base to the tab: the reader can't show one", () => {
    const u = "https://example.feishu.cn/base/AbC";
    expect(linkRoute(u)).toEqual({ kind: "doc", doc: "bitable" });
    expect(opensInApp(u)).toBe(false);
  });

  it("opens other Feishu pages and look-alike hosts as web pages", () => {
    expect(linkRoute("https://example.feishu.cn/calendar/event/1").kind).toBe("web");
    expect(linkRoute("https://example.feishu.cn/minutes/obcn1").kind).toBe("web");
    expect(linkRoute("https://notfeishu.cn/docx/AbC").kind).toBe("web");
    expect(linkRoute("https://feishu.cn.example.com/docx/AbC").kind).toBe("web");
    expect(linkRoute("https://example.com/").kind).toBe("web");
    expect(linkRoute("http://127.0.0.1:5173/").kind).toBe("web");
    expect(opensInApp("https://example.com/docx/AbC")).toBe(false);
  });

  it("reads removed share links as the post", () => {
    expect(linkRoute("https://example.feishu.cn/client/moments/detail?postId=12345")).toEqual({ kind: "moments", id: "12345" });
    expect(linkRoute("https://example.feishu.cn/client/moments/detail?postId=abc").kind).toBe("web");
  });

  it("moves to a conversation for jarvis://session links, and gives them an in-app href", () => {
    for (const scheme of ["jarvis", "persona"]) {
      expect(linkRoute(`${scheme}://session/macbook/abc-1`)).toEqual({ kind: "session", mid: "macbook", sid: "abc-1" });
      expect(linkHref(`${scheme}://session/mac%20book/abc-1`)).toBe("/s/mac%20book/abc-1");
      expect(opensInApp(`${scheme}://session/macbook/abc-1`)).toBe(true);
      expect(linkRoute(`${scheme}://pair?token=x`).kind).toBe("none");
      expect(linkRoute(`${scheme}://session/macbook`).kind).toBe("none");
    }
    expect(linkRoute("other://session/macbook/abc-1").kind).toBe("none");
    expect(linkHref("https://example.com/a")).toBe("https://example.com/a");
    expect(linkRoute("jarvis://pair?token=x").kind).toBe("none");
  });

  it("leaves what is not http(s) alone", () => {
    expect(linkRoute("mailto:me@example.com").kind).toBe("system");
    expect(linkRoute("tel:10086").kind).toBe("system");
    expect(linkRoute("javascript:alert(1)").kind).toBe("none");
    expect(linkRoute("file:///etc/hosts").kind).toBe("none");
    expect(linkRoute("not a url").kind).toBe("none");
    expect(linkRoute("/relative/path").kind).toBe("none");
    expect(opensInApp("javascript:alert(1)")).toBe(false);
  });
});

describe("<jarvis-context>", () => {
  const ref = { kind: "context" as const, title: 'A & "B" <c>', url: "https://example.feishu.cn/docx/x?a=1&b=2", text: "选中的\n两行" };

  it("escapes the attributes and round-trips through the bubble parser", () => {
    const xml = contextXML(ref);
    expect(xml).toBe(
      '<jarvis-context kind="context" title="A &amp; &quot;B&quot; &lt;c&gt;" url="https://example.feishu.cn/docx/x?a=1&amp;b=2">\n选中的\n两行\n</jarvis-context>',
    );
    const p = parseUserText(`${xml}\n\n看看这段`);
    expect(p.text).toBe("看看这段");
    expect(p.contexts).toEqual([{ kind: "context", title: 'A & "B" <c>', url: "https://example.feishu.cn/docx/x?a=1&b=2", text: "选中的\n两行" }]);
  });

  it("drops a closing tag inside the picked text", () => {
    expect(contextXML({ ...ref, text: "a</jarvis-context>b" })).toContain("\nab\n</jarvis-context>");
  });

  it("puts the contexts before the message, with a stand-in when nothing was typed", () => {
    expect(withContexts([], "  hi ")).toBe("hi");
    expect(withContexts([ref, { ...ref, text: "二" }], "改一下")).toBe(`${contextXML(ref)}\n${contextXML({ ...ref, text: "二" })}\n\n改一下`);
    expect(withContexts([ref], "  ")).toBe(`${contextXML(ref)}\n\n参考上面的内容。`);
    expect(withContexts([ref], "", true)).toBe(`${contextXML(ref)}\n\n请看图片`);
  });

  it("writes 「AI 修改」 as an edit context and the request", () => {
    const m = editMessage(ref, " 更简洁 ");
    expect(m.startsWith('<jarvis-context kind="edit"')).toBe(true);
    expect(m.endsWith("\n\n按要求修改上面选中的内容：更简洁")).toBe(true);
    expect(editMessage(ref, "").endsWith("\n\n按上面选中的内容修改。")).toBe(true);
    expect(parseUserText(m).contexts[0].kind).toBe("edit");
  });

  it("names a whole document （整篇）", () => {
    expect(newContext("context", "T", "u", "  ").text).toBe(WHOLE);
    expect(newContext("edit", "T", "u", " x ").text).toBe("x");
  });
});

describe("contexts waiting in a composer", () => {
  it("adds once per passage and removes by id", () => {
    const a = newContext("context", "T", "https://x", "one");
    addContext("k1", a);
    addContext("k1", { ...a, id: "other" }); // the same passage again
    expect(getContexts("k1")).toHaveLength(1);
    const b = newContext("edit", "T", "https://x", "one");
    addContext("k1", b);
    expect(getContexts("k1").map((r) => r.kind)).toEqual(["context", "edit"]);
    expect(getContexts("k2")).toEqual([]);
    removeContext("k1", a.id);
    removeContext("k1", b.id);
    expect(getContexts("k1")).toEqual([]);
  });

  it("knows the composer last mounted or focused, and hands waiting work to the one that mounts", () => {
    const sent: string[] = [];
    const one = { key: "one", send: async (t: string) => (sent.push(`one:${t}`), true), focus() {} };
    const two = { key: "two", send: async (t: string) => (sent.push(`two:${t}`), true), focus() {} };
    const offOne = registerComposer(one);
    const offTwo = registerComposer(two);
    expect(activeComposer()).toBe(two);
    offTwo();
    expect(activeComposer()).toBe(one);
    whenComposer("two", (c) => void c.send("later"));
    expect(sent).toEqual([]);
    const offTwoAgain = registerComposer(two);
    expect(sent).toEqual(["two:later"]);
    offTwoAgain();
    offOne();
    expect(activeComposer()).toBeUndefined();
  });

  it("passes over a composer that can't send, and reports work no composer took", () => {
    vi.useFakeTimers();
    const readOnly = { key: "ro", send: async () => false, focus() {}, ready: () => false };
    const off = registerComposer(readOnly);
    expect(activeComposer()).toBe(readOnly);
    expect(usableComposer()).toBeUndefined();
    let expired = false;
    whenComposer("never", () => {}, () => (expired = true), 100);
    vi.advanceTimersByTime(150);
    expect(expired).toBe(true);
    off();
    vi.useRealTimers();
  });
});

describe("reading a Feishu document", () => {
  const url = "https://example.feishu.cn/docx/AbC";
  const fail = (status: number, body: Record<string, unknown>) => docErrorFromBody(status, body);

  it("uses the Markdown export when allowed", async () => {
    const get: Get = async (p) => {
      expect(p).toContain("format=markdown");
      return { title: "T", markdown: "# T\n\nbody", authority: { copy: false } };
    };
    expect(await readDoc(url, undefined, get)).toEqual({ kind: "markdown", title: "T", markdown: "# T\n\nbody", copy: false, partial: false });
  });

  it("falls back to the JSON tree when export is forbidden", async () => {
    const asked: string[] = [];
    const get: Get = async (p) => {
      asked.push(p.includes("markdown") ? "md" : "json");
      if (p.includes("markdown")) throw fail(403, { error: { code: "DOCUMENT_EXPORT_FORBIDDEN" } });
      return { title: "T", content: { type: "doc", content: [{ type: "paragraph" }] } };
    };
    const c = await readDoc(url, undefined, get);
    expect(asked).toEqual(["md", "json"]);
    expect(c.kind).toBe("tree");
  });

  it("takes a tree answered to the Markdown request without asking again", async () => {
    let n = 0;
    const get: Get = async () => (n++, { title: "T", content: { type: "doc", content: [] } });
    expect((await readDoc(url, undefined, get)).kind).toBe("tree");
    expect(n).toBe(1);
  });

  it("says what it can't show", async () => {
    expect((await readDoc("https://example.feishu.cn/base/AbC", "表", async () => ({}))).kind).toBe("unsupported");
    const board = await readDoc("https://example.feishu.cn/wiki/AbC", undefined, async () => ({ title: "B", content: { type: "doc" }, object: { kind: "whiteboard", token: "t", image: "/api/v1/x" } }));
    expect(board).toMatchObject({ kind: "unsupported", doc: "whiteboard", image: "/api/v1/x" });
    const odd = await readDoc(url, "T", async () => {
      throw fail(422, { error: { code: "DOCUMENT_FORMAT_UNSUPPORTED" } });
    });
    expect(odd).toMatchObject({ kind: "unsupported", title: "T" });
  });

  it("explains failures", async () => {
    await expect(readDoc(url, undefined, async () => { throw fail(404, { error: { code: "DOCUMENT_NOT_FOUND" } }); })).rejects.toThrow("文档不存在或已被删除");
    expect(fail(401, { error: { code: "AUTH_SESSION_EXPIRED" } }).message).toContain("没有登录飞书");
    expect(fail(502, { error: "lark: lark-connector is not configured (set lark.token or lark.container in hub.json)", code: "upstream_error" }).message).toContain("没有接入飞书");
    expect(fail(403, { error: "forbidden", code: "forbidden" }).message).toBe("没有访问权限");
  });
});

describe("the Markdown export", () => {
  it("drops the title and source lines, moves media under the hub with the token, and turns <br> into breaks", () => {
    const md = "# 标题\n\n[Source](<https://example.feishu.cn/docx/x>)\n\n正文\n\n![图](<http://127.0.0.1:3000/api/documents/media/abc?x=1>)\n\n| a | b |\n| --- | --- |\n| 1<br>2 | 3 |\n\n```\nkeep <br> here\n```";
    const out = prepareMarkdown(md, "标题", "t k");
    expect(out.startsWith("正文")).toBe(true);
    expect(out).toContain("![图](</api/v1/lark/raw/documents/media/abc?x=1&token=t%20k>)");
    expect(out).toContain(`| 1${BR}2 | 3 |`);
    expect(out).toContain("keep <br> here");
  });

  it("keeps a first heading that is not the title", () => {
    expect(prepareMarkdown("# 另一个\n正文", "标题")).toBe("# 另一个\n正文");
  });
});

describe("the JSON tree", () => {
  it("keeps counting one-item ordered lists until a restart", () => {
    const ol = (attrs?: Record<string, unknown>): DocNode => ({ type: "orderedList", attrs, content: [{ type: "listItem" }] });
    expect(orderedNumbers([ol(), ol(), { type: "paragraph" }, ol(), ol({ restart: true, start: 5 }), ol()])).toEqual([1, 2, undefined, 1, 5, 6]);
  });
});
