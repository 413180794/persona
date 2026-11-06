// The build's check that sw.js stays a classic script (#5070): an import in it fails `npm run build`.
import { describe, expect, it } from "vitest";
import { classicWorkerProblem } from "./sw-entry-check";

describe("sw.js 不能有 import", () => {
  it("没有 import 的 worker 照常构建", () => {
    expect(classicWorkerProblem(`(function(){"use strict";const e="persona-shell";self.addEventListener("fetch",()=>{});})();`)).toBeUndefined();
    expect(classicWorkerProblem(`const a=import.meta.url;const important=1;x.import("y");`)).toBeUndefined();
  });

  it("开头是 import（共享 chunk）就报错，说明原因和怎么修", () => {
    const msg = classicWorkerProblem(`import{N as a}from"./assets/notify-abc.js";self.x=a;`);
    expect(msg).toContain("classic service worker");
    expect(msg).toContain("docs/lessons/web.md");
  });

  it("中间的 import 语句、只为副作用的 import、带默认名的 import 也算", () => {
    expect(classicWorkerProblem(`"use strict";\nimport "./assets/a.js";`)).toBeDefined();
    expect(classicWorkerProblem(`const x=1;import d,{e}from'./assets/b.js'`)).toBeDefined();
    expect(classicWorkerProblem(`import * as m from "./assets/c.js"`)).toBeDefined();
  });

  it("Rollup 记下的 import 列表不空就报错，并写出是哪个文件", () => {
    expect(classicWorkerProblem(`self.x=1`, ["assets/shared-123.js"])).toContain("assets/shared-123.js");
  });
});
