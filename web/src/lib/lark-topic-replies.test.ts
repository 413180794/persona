import { describe, expect, it } from "vitest";
import type { LarkMessage } from "./inbox";
import { LarkTopicReplies, PARALLEL, topicPreview, topicReplyCount } from "./lark-topic-replies";

const topic = (id: string, replies: number): LarkMessage => ({ id, position: id, reply_count: replies });
const msg = (id: string): LarkMessage => ({ id, position: id, text: id });

function deferred<T>() {
  let resolve!: (v: T) => void, reject!: (e: unknown) => void;
  const promise = new Promise<T>((a, b) => ((resolve = a), (reject = b)));
  return { promise, resolve, reject };
}

const tick = () => new Promise((r) => setTimeout(r, 0));

describe("lark topic replies", () => {
  it("counts the larger of Feishu's two counts", () => {
    expect(topicReplyCount({ id: "a", position: "1", reply_count: 2, thread_reply_count: 5 })).toBe(5);
    expect(topicReplyCount({ id: "a", position: "1" })).toBe(0);
  });

  it("keeps the newest two replies, the topic left out", () => {
    const p = topicPreview([msg("t"), msg("r1"), msg("r2"), msg("r3")], "t", 7);
    expect(p.replies.map((m) => m.id)).toEqual(["r2", "r3"]);
    expect(p.earlier).toBe(5);
  });

  it("reads a few topics at a time and the rest as reads come in", async () => {
    const calls: string[] = [];
    const waits = new Map<string, ReturnType<typeof deferred<LarkMessage[]>>>();
    const q = new LarkTopicReplies((id) => {
      calls.push(id);
      const d = deferred<LarkMessage[]>();
      waits.set(id, d);
      return d.promise;
    });
    for (const id of ["a", "b", "c", "d", "e"]) q.want(topic(id, 1));
    expect(calls).toEqual(["a", "b", "c"].slice(0, PARALLEL));
    waits.get("a")!.resolve([msg("a"), msg("a1")]);
    await tick();
    expect(calls).toContain("d");
    expect(q.preview("a")?.replies.map((m) => m.id)).toEqual(["a1"]);
  });

  it("reads again only when the count changes, or after a failure on retry", async () => {
    let n = 0;
    let fail = true;
    const q = new LarkTopicReplies(async () => {
      n++;
      if (fail) throw new Error("down");
      return [msg("r")];
    });
    q.want(topic("t", 1));
    await tick();
    expect(q.preview("t")?.failed).toBe(true);
    fail = false;
    q.retry(topic("t", 1));
    await tick();
    expect(q.preview("t")?.count).toBe(1);
    expect(q.preview("t")?.failed).toBeFalsy();
    q.want(topic("t", 1));
    await tick();
    expect(n).toBe(2);
    q.want(topic("t", 2));
    await tick();
    expect(n).toBe(3);
  });

  it("reads again after a read under way when the count changed meanwhile", async () => {
    const d = deferred<LarkMessage[]>();
    let n = 0;
    const q = new LarkTopicReplies(() => (++n === 1 ? d.promise : Promise.resolve([msg("x")])));
    q.want(topic("t", 1));
    q.want(topic("t", 2));
    d.resolve([msg("r")]);
    await tick();
    await tick();
    expect(n).toBe(2);
    expect(q.preview("t")?.count).toBe(2);
  });

  it("skips topics dropped before their turn and tells listeners when a read lands", async () => {
    const calls: string[] = [];
    const hold = deferred<LarkMessage[]>();
    const q = new LarkTopicReplies((id) => (calls.push(id), hold.promise));
    let told = 0;
    q.subscribe(() => told++);
    for (const id of ["a", "b", "c", "d"]) q.want(topic(id, 1));
    q.drop("d");
    hold.resolve([]);
    await tick();
    expect(calls).toEqual(["a", "b", "c"]);
    expect(told).toBe(3);
  });

  it("starts nothing while closed and goes on when opened again", async () => {
    const calls: string[] = [];
    const q = new LarkTopicReplies(async (id) => (calls.push(id), []));
    q.close();
    q.want(topic("a", 1));
    expect(calls).toEqual([]);
    q.open();
    q.want(topic("a", 1));
    await tick();
    expect(calls).toEqual(["a"]);
  });
});
