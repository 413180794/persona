import { describe, expect, it } from "vitest";
import { computerView, computerWatching, GRANT_HINT, missingPermissions, screenPath, type MachineComputer } from "./computer";

const base: MachineComputer = { enabled: true, installed: true, version: "0.38.0", daemon: true, accessibility: "granted", screen_recording: "granted", desktop: "active", ready: true, screen_sharing: true };

describe("computer section", () => {
  it("shows nothing below the switch while off, and offline first", () => {
    expect(computerView({ enabled: false }, true)).toEqual({ kind: "off" });
    expect(computerView(base, false)).toEqual({ kind: "offline" });
  });

  it("is ready with the version and the screen when Screen Sharing is on", () => {
    expect(computerView(base, true)).toEqual({ kind: "ready", text: "现在可以操控 · Cua Driver 0.38.0", screen: true });
    expect(computerView({ ...base, backend: "lcu", version: "0.13.5" }, true)).toEqual({ kind: "ready", text: "现在可以操控 · LCU 0.13.5", screen: true });
    expect(computerView({ enabled: true, backend: "lcu", installed: false }, true)).toEqual({ kind: "install", text: "这台 Mac 上还没有装 LCU", retry: false });
    expect(computerView({ ...base, screen_sharing: false }, true)).toMatchObject({ kind: "ready", screen: false });
  });

  it("offers the install, and says why one failed", () => {
    expect(computerView({ enabled: true, installed: false }, true)).toEqual({ kind: "install", text: "这台 Mac 上还没有装 Cua Driver", retry: false });
    const failed = computerView({ enabled: true, installed: false, job: { kind: "install", state: "failed", error: "校验没过" } }, true);
    expect(failed).toEqual({ kind: "install", text: "没装上：校验没过", retry: true });
  });

  it("shows a running job's step, cancel only for an install", () => {
    expect(computerView({ enabled: true, installed: false, job: { kind: "install", state: "running", step: "下载安装器" } }, true)).toEqual({ kind: "running", text: "下载安装器", cancel: true, screen: false });
    const grant = computerView({ ...base, ready: false, accessibility: "missing", job: { kind: "grant", state: "running" } }, true);
    expect(grant).toEqual({ kind: "running", text: GRANT_HINT, cancel: false, screen: true });
  });

  it("asks for the missing permissions, then waits for 允许 on the Mac", () => {
    const c: MachineComputer = { ...base, ready: false, accessibility: "missing", screen_recording: "missing" };
    expect(missingPermissions(c)).toEqual(["辅助功能", "屏幕录制"]);
    expect(computerView(c, true)).toEqual({ kind: "grant", text: "Cua Driver 还没有辅助功能、屏幕录制权限" });
    expect(computerView({ ...c, job: { kind: "grant", state: "failed", error: "没有弹窗" } }, true)).toEqual({ kind: "grant", text: "没有弹窗" });
    const done = { ...c, job: { kind: "grant", state: "done" } };
    expect(computerView(done, true)).toEqual({ kind: "granted", screen: true });
    expect(computerWatching(done, true)).toBe(true);
  });

  it("falls back to the hub's note", () => {
    expect(computerView({ ...base, ready: false, desktop: "locked", note: "这台 Mac 锁屏了，解锁后才能操控" }, true)).toEqual({ kind: "unknown", text: "这台 Mac 锁屏了，解锁后才能操控" });
  });

  it("watches only while something moves", () => {
    expect(computerWatching(base, true)).toBe(false);
    expect(computerWatching({ ...base, job: { kind: "install", state: "running" } }, true)).toBe(true);
    expect(computerWatching({ ...base, job: { kind: "install", state: "running" } }, false)).toBe(false);
    expect(computerWatching({ ...base, job: { kind: "grant", state: "done" } }, true)).toBe(false);
  });

  it("links the live screen", () => {
    expect(screenPath("mac 1")).toBe("/screen/mac%201");
  });
});
