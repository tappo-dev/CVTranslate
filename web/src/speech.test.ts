import { describe, expect, it, vi } from "vitest";
import { SpeechController, type SpeechAudio } from "./speech";

function deferred<Value>() {
  let resolve!: (value: Value) => void;
  let reject!: (error: unknown) => void;
  const promise = new Promise<Value>((resolvePromise, rejectPromise) => {
    resolve = resolvePromise;
    reject = rejectPromise;
  });
  return { promise, resolve, reject };
}
function setup() {
  const audio: SpeechAudio = {
    src: "", onended: null, onerror: null,
    play: vi.fn().mockResolvedValue(undefined), pause: vi.fn(), load: vi.fn(), removeAttribute: vi.fn(),
  };
  const dependencies = {
    fetchAudio: vi.fn().mockResolvedValue(new Blob(["audio"], { type: "audio/mpeg" })),
    createAudio: vi.fn(() => audio), createUrl: vi.fn(() => "blob:test"), revokeUrl: vi.fn(),
  };
  const changed = vi.fn();
  const controller = new SpeechController(dependencies, changed);
  return { audio, dependencies, changed, controller };
}

describe("Edge speech lifecycle", () => {
  it("cancels loading on the same target and ignores a late response without creating a URL", async () => {
    const { controller, dependencies, changed } = setup();
    const pending = deferred<Blob>();
    dependencies.fetchAudio.mockReturnValueOnce(pending.promise);
    const first = controller.speak("source", "Hello", "auto");
    const signal = dependencies.fetchAudio.mock.calls[0][2] as AbortSignal;
    expect(changed).toHaveBeenLastCalledWith({ activeTarget: "source", status: "loading", error: "" });
    await controller.speak("source", "Hello", "auto");
    expect(signal.aborted).toBe(true);
    pending.resolve(new Blob(["audio"], { type: "audio/mpeg" }));
    await first;
    expect(dependencies.createUrl).not.toHaveBeenCalled();
    expect(changed).toHaveBeenLastCalledWith({ activeTarget: null, status: "stopped", error: "" });
  });

  it("switches targets and ignores stale events and play rejection while releasing every URL", async () => {
    const { controller, dependencies, audio, changed } = setup();
    const playing = deferred<void>();
    vi.mocked(audio.play).mockReturnValueOnce(playing.promise);
    const first = controller.speak("source", "Hello", "en");
    await Promise.resolve();
    const staleEnd = audio.onended;
    const staleError = audio.onerror;
    await controller.speak("translation", "你好", "zh-CN");
    staleEnd?.(new Event("ended"));
    staleError?.(new Event("error"));
    playing.reject(new DOMException("blocked", "NotAllowedError"));
    await first;
    expect(changed).toHaveBeenLastCalledWith({ activeTarget: "translation", status: "playing", error: "" });
    expect(dependencies.revokeUrl).toHaveBeenCalledTimes(1);
    audio.onended?.(new Event("ended"));
    expect(dependencies.revokeUrl).toHaveBeenCalledTimes(2);
    expect(audio.onended).toBeNull();
    expect(audio.onerror).toBeNull();
    expect(audio.removeAttribute).toHaveBeenCalledWith("src");
    controller.dispose();
    expect(dependencies.revokeUrl).toHaveBeenCalledTimes(2);
  });

  it("releases resources and gives actionable playback denial without retrying", async () => {
    const { controller, dependencies, audio, changed } = setup();
    vi.mocked(audio.play).mockRejectedValue(new DOMException("blocked", "NotAllowedError"));
    await controller.speak("source", "Hello", "en");
    expect(changed.mock.lastCall?.[0].error).toContain("允许页面播放音频");
    expect(dependencies.revokeUrl).toHaveBeenCalledOnce();
    expect(dependencies.fetchAudio).toHaveBeenCalledOnce();
  });

  it("stops playback on the same target instead of sending another request", async () => {
    const { controller, dependencies, audio, changed } = setup();
    await controller.speak("source", "Hello", "en");
    await controller.speak("source", "Hello", "en");
    expect(dependencies.fetchAudio).toHaveBeenCalledOnce();
    expect(audio.pause).toHaveBeenCalledOnce();
    expect(dependencies.revokeUrl).toHaveBeenCalledOnce();
    expect(changed).toHaveBeenLastCalledWith({ activeTarget: null, status: "stopped", error: "" });
  });

  it("aborts a pending request when switching targets and ignores its late completion", async () => {
    const { controller, dependencies, changed } = setup();
    const pending = deferred<Blob>();
    dependencies.fetchAudio.mockReturnValueOnce(pending.promise);
    const first = controller.speak("source", "Hello", "auto");
    const signal = dependencies.fetchAudio.mock.calls[0][2] as AbortSignal;
    await controller.speak("translation", "你好", "zh-CN");
    expect(signal.aborted).toBe(true);
    pending.resolve(new Blob(["audio"], { type: "audio/mpeg" }));
    await first;
    expect(dependencies.createUrl).toHaveBeenCalledOnce();
    expect(changed).toHaveBeenLastCalledWith({ activeTarget: "translation", status: "playing", error: "" });
    controller.dispose();
    expect(dependencies.revokeUrl).toHaveBeenCalledOnce();
  });

  it("disposes a pending request without updates and permits a fresh lifecycle", async () => {
    const { controller, dependencies, changed } = setup();
    const pending = deferred<Blob>();
    dependencies.fetchAudio.mockReturnValueOnce(pending.promise);
    const first = controller.speak("source", "Hello", "en");
    const signal = dependencies.fetchAudio.mock.calls[0][2] as AbortSignal;
    controller.dispose();
    const updateCount = changed.mock.calls.length;
    pending.reject(new Error("late failure"));
    await first;
    expect(signal.aborted).toBe(true);
    expect(changed).toHaveBeenCalledTimes(updateCount);
    await controller.speak("source", "Hello", "en");
    controller.dispose();
    expect(dependencies.revokeUrl).toHaveBeenCalledOnce();
  });

  it("rejects invalid media and over-limit Unicode input before playback", async () => {
    const { controller, dependencies } = setup();
    await controller.speak("source", "😀".repeat(20001), "auto");
    expect(dependencies.fetchAudio).not.toHaveBeenCalled();
    dependencies.fetchAudio.mockResolvedValueOnce(new Blob(["not audio"], { type: "application/json" }));
    await controller.speak("source", "Hello", "en");
    expect(dependencies.createUrl).not.toHaveBeenCalled();
    expect(dependencies.createAudio).not.toHaveBeenCalled();
  });
});
