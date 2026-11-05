import { describe, expect, it } from "vitest";
import { insertPerson, mentionsIn, personAt } from "./lark-people";

const li = { user_id: "ou_1", name: "李雷" };
const han = { user_id: "ou_2", name: "韩梅梅" };
const short = { user_id: "ou_3", name: "李" };

describe("personAt：正在输入的 @名字", () => {
  it("开头、空格后、中文后都算", () => {
    expect(personAt("@李", 2)).toEqual({ start: 0, end: 2, query: "李" });
    expect(personAt("请 @韩", 4)).toEqual({ start: 2, end: 4, query: "韩" });
    expect(personAt("请@韩", 3)).toEqual({ start: 1, end: 3, query: "韩" });
    expect(personAt("看看@", 3)).toEqual({ start: 2, end: 3, query: "" });
  });

  it("邮箱、名字后有空格、光标不在名字末尾都不算", () => {
    expect(personAt("me@example.com", 14)).toBeNull();
    expect(personAt("@李雷 好的", 6)).toBeNull();
    expect(personAt("没有艾特", 4)).toBeNull();
  });
});

describe("insertPerson：选中的人填进去", () => {
  it("换掉 @查询，后面补一个空格，光标放在空格后", () => {
    const text = "请@李 看看";
    const out = insertPerson(text, personAt(text, 3)!, li);
    expect(out.text).toBe("请@李雷 看看");
    expect(out.text.slice(0, out.caret)).toBe("请@李雷 ");
  });

  it("在末尾也补空格", () => {
    const out = insertPerson("@韩", personAt("@韩", 2)!, han);
    expect(out).toEqual({ text: "@韩梅梅 ", caret: 5 });
  });
});

describe("mentionsIn：发出去时还 @ 着谁", () => {
  it("文字里删掉的 @ 不再带上", () => {
    expect(mentionsIn("@李雷 你看下", [li, han])).toEqual([li]);
  });

  it("同一个人选了两次只带一次", () => {
    expect(mentionsIn("@李雷 @李雷", [li, li])).toEqual([li]);
  });

  it("长名字先数：@李雷 不算 @李", () => {
    expect(mentionsIn("@李雷 好", [short, li])).toEqual([li]);
    expect(mentionsIn("@李雷 和 @李", [short, li])).toEqual([short, li]);
  });

  it("没选过的 @名字 不带", () => {
    expect(mentionsIn("@王五 好", [li])).toEqual([]);
  });
});
