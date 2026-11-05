import { describe, expect, it } from "vitest";
import { ApiError } from "./api";
import { accountTitle, larkPath, LarkAccountsStore, olAccount, type LarkAccount, type LarkAccountList } from "./lark-accounts";

const account = (id: number, name: string, signed_in = true): LarkAccount => ({ id, name, signed_in, unread: id });

function store(answer: () => Promise<LarkAccountList>, saved?: number) {
  let kept = saved;
  const s = new LarkAccountsStore({ list: answer, load: () => kept, save: (id) => (kept = id) });
  return { s, kept: () => kept };
}

describe("larkPath", () => {
  it("names another account, never the hub's own, and keeps one already named", () => {
    expect(larkPath("/lark/conversations/f/messages", 2)).toBe("/lark/conversations/f/messages?ol_account=2");
    expect(larkPath("/lark/conversations/f/messages?limit=30", 2)).toBe("/lark/conversations/f/messages?limit=30&ol_account=2");
    expect(larkPath("/lark/conversations/f/messages?limit=30", undefined)).toBe("/lark/conversations/f/messages?limit=30");
    expect(larkPath("/lark/media/k?ol_account=3", 2)).toBe("/lark/media/k?ol_account=3");
  });

  it("reads ol_account and titles", () => {
    expect([olAccount(2), olAccount(0), olAccount(-1), olAccount("2"), olAccount(1.5), olAccount(undefined)]).toEqual([2, undefined, undefined, undefined, undefined, undefined]);
    expect(accountTitle({ id: 3, name: " " })).toBe("账号 3");
  });
});

describe("LarkAccountsStore", () => {
  it("chooses another account, keeps the choice, and falls back when it signs out", async () => {
    let list: LarkAccountList = { accounts: [account(1, "一号"), account(2, "二号"), account(3, "三号", false)], default: 1 };
    const { s, kept } = store(async () => list);
    await s.refresh();
    expect(s.state.accounts.map((a) => a.id)).toEqual([1, 2]); // signed out: no chip
    expect(s.state).toMatchObject({ selected: 1, request: undefined, selectedName: "一号" });
    s.select(2);
    expect(s.state).toMatchObject({ selected: 2, request: 2, selectedName: "二号" });
    expect(kept()).toBe(2);
    // the next tab starts on the kept choice
    const again = store(async () => list, kept()).s;
    await again.refresh();
    expect(again.state.request).toBe(2);
    // the chosen one signed out, one account left: the hub's own, no name
    list = { accounts: [account(1, "一号"), account(2, "二号", false)], default: 1 };
    await again.refresh();
    expect(again.state).toMatchObject({ selected: 1, request: undefined, selectedName: undefined });
  });

  it("the hub's own account is never named, whichever it is", async () => {
    const { s } = store(async () => ({ accounts: [account(1, "一号"), account(2, "二号")], default: 2 }));
    await s.refresh();
    expect(s.state).toMatchObject({ selected: 2, request: undefined });
    s.select(1);
    expect(s.state.request).toBe(1);
  });

  it("an older hub (404) has no accounts; another failure keeps the last list", async () => {
    let fail: Error = new ApiError(503, "busy", {});
    let first = true;
    const { s } = store(async () => {
      if (first) {
        first = false;
        return { accounts: [account(1, "一号"), account(2, "二号")], default: 1 };
      }
      throw fail;
    }, 2);
    await s.refresh();
    expect(s.state.request).toBe(2);
    await s.refresh();
    expect(s.state.accounts.length).toBe(2);
    fail = new ApiError(404, "not found", {});
    await s.refresh();
    expect(s.state).toMatchObject({ accounts: [], request: undefined, selected: undefined });
  });

  it("an older read that lands after a newer one does not put back its unread counts (#4136)", async () => {
    const turns: ((list: LarkAccountList) => void)[] = [];
    const { s } = store(() => new Promise<LarkAccountList>((r) => turns.push(r)));
    const older = s.refresh();
    const newer = s.refresh();
    turns[1]({ accounts: [{ ...account(1, "一号"), unread: 5 }], default: 1 });
    await newer;
    turns[0]({ accounts: [{ ...account(1, "一号"), unread: 0 }], default: 1 });
    await older;
    expect(s.state.accounts.map((a) => a.unread)).toEqual([5]);
  });

  it("an older 404 that lands after a newer list does not take the accounts away", async () => {
    const turns: { ok: (list: LarkAccountList) => void; fail: (e: unknown) => void }[] = [];
    const { s } = store(() => new Promise<LarkAccountList>((ok, fail) => turns.push({ ok, fail })));
    const older = s.refresh();
    const newer = s.refresh();
    turns[1].ok({ accounts: [account(1, "一号"), account(2, "二号")], default: 1 });
    await newer;
    turns[0].fail(new ApiError(404, "not found", {}));
    await older;
    expect(s.state.accounts.map((a) => a.id)).toEqual([1, 2]);
  });
});
