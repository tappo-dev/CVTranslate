import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { request } from "./api";
import { getShortcutActionLabel, getShortcutStageLabel, watchPlatformStatus } from "./shortcut-status";
import type { AppState } from "./types";
import { createTestAppState } from "./speech-test-fixtures";

vi.mock("./api", () => ({ request: vi.fn() }));

describe("shortcut feedback", () => {
  it("distinguishes capture failure from no trigger and dispatch from insertion", () => {
    expect(getShortcutStageLabel("capture_failed")).toContain("不是快捷键未触发");
    expect(getShortcutStageLabel("paste_dispatched")).toContain("不代表应用已插入文字");
    expect(getShortcutStageLabel("direct_paste_dispatched")).toContain("未核验原目标");
    expect(getShortcutStageLabel("direct_paste_dispatched")).toContain("不代表应用已插入文字");
    expect(getShortcutStageLabel("direct_pasting")).toContain("不核验粘贴目标");
    expect(getShortcutStageLabel("direct_translating")).toContain("前台窗口");
    expect(getShortcutStageLabel("direct_paste_skipped")).toContain("此模式不检查原目标");
    expect(getShortcutStageLabel("paste_skipped")).toContain("手动复制译文");
    expect(getShortcutActionLabel("selection-ui")).toBe("未知快捷键动作");
    expect(getShortcutActionLabel("open-ui")).toBe("打开 WebUI（不翻译）");
    expect(getShortcutStageLabel("future_stage")).toContain("未知处理阶段");
    expect(getShortcutStageLabel("toString")).toContain("未知处理阶段");
  });
});

describe("platform-only status polling", () => {
  let documentEvents: EventTarget & { visibilityState: string };
  let windowEvents: EventTarget;
  let watcher: ReturnType<typeof watchPlatformStatus> | undefined;
  const response = createTestAppState();

  beforeEach(() => {
    vi.useFakeTimers();
    vi.mocked(request).mockReset();
    documentEvents = Object.assign(new EventTarget(), { visibilityState: "visible" });
    windowEvents = new EventTarget();
    vi.stubGlobal("document", documentEvents);
    vi.stubGlobal("window", windowEvents);
  });

  afterEach(() => {
    watcher?.stop();
    watcher = undefined;
    vi.unstubAllGlobals();
    vi.useRealTimers();
  });

  it("shares a request across focus/manual refresh and only publishes platform", async () => {
    let resolveRequest!: (value: AppState) => void;
    vi.mocked(request).mockImplementation(() => new Promise<AppState>((resolve) => { resolveRequest = resolve; }));
    const onPlatform = vi.fn();
    watcher = watchPlatformStatus(onPlatform, vi.fn());
    const refresh = watcher.refresh();
    windowEvents.dispatchEvent(new Event("focus"));
    await vi.advanceTimersByTimeAsync(5000);
    expect(request).toHaveBeenCalledTimes(1);
    resolveRequest(response);
    await refresh;
    expect(onPlatform).toHaveBeenCalledExactlyOnceWith(response.platform);
    await vi.advanceTimersByTimeAsync(1499);
    expect(request).toHaveBeenCalledTimes(1);
    await vi.advanceTimersByTimeAsync(1);
    expect(request).toHaveBeenCalledTimes(2);
  });

  it("does not poll while hidden and coalesces visibility/focus refresh", async () => {
    documentEvents.visibilityState = "hidden";
    vi.mocked(request).mockResolvedValue(response);
    watcher = watchPlatformStatus(vi.fn(), vi.fn());
    await vi.advanceTimersByTimeAsync(5000);
    expect(request).not.toHaveBeenCalled();
    documentEvents.visibilityState = "visible";
    documentEvents.dispatchEvent(new Event("visibilitychange"));
    windowEvents.dispatchEvent(new Event("focus"));
    await vi.advanceTimersByTimeAsync(0);
    expect(request).toHaveBeenCalledTimes(1);
    documentEvents.visibilityState = "hidden";
    documentEvents.dispatchEvent(new Event("visibilitychange"));
    await vi.advanceTimersByTimeAsync(5000);
    expect(request).toHaveBeenCalledTimes(1);
  });

  it("aborts on stop and ignores a late response", async () => {
    let resolveRequest!: (value: AppState) => void;
    vi.mocked(request).mockImplementation(() => new Promise<AppState>((resolve) => { resolveRequest = resolve; }));
    const onPlatform = vi.fn();
    const onError = vi.fn();
    watcher = watchPlatformStatus(onPlatform, onError);
    const refresh = watcher.refresh();
    const signal = vi.mocked(request).mock.calls[0][1]?.signal;
    watcher.stop();
    expect(signal?.aborted).toBe(true);
    resolveRequest(response);
    await refresh;
    await vi.advanceTimersByTimeAsync(5000);
    expect(onPlatform).not.toHaveBeenCalled();
    expect(onError).not.toHaveBeenCalled();
    expect(request).toHaveBeenCalledTimes(1);
  });

  it("keeps failures non-blocking and clears the warning after recovery", async () => {
    vi.mocked(request).mockRejectedValueOnce(new Error("offline")).mockResolvedValue(response);
    const onPlatform = vi.fn();
    const onError = vi.fn();
    watcher = watchPlatformStatus(onPlatform, onError);
    await watcher.refresh();
    expect(onPlatform).not.toHaveBeenCalled();
    expect(onError).toHaveBeenLastCalledWith(expect.stringContaining("未保存的设置"));
    await vi.advanceTimersByTimeAsync(1500);
    expect(onPlatform).toHaveBeenCalledExactlyOnceWith(response.platform);
    expect(onError).toHaveBeenLastCalledWith("");
  });
});
