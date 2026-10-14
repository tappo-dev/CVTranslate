import { afterEach, describe, expect, it, vi } from "vitest";
import { getBrowserSynthesis, getMatchingVoices, selectBrowserVoice, watchBrowserVoices } from "./browser-voices";
import { createTestSynthesis, createTestVoice, TestUtterance } from "./speech-test-fixtures";

afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

describe("browser voice language selection", () => {
  const voices = [
    createTestVoice("english-first", "en-US"),
    createTestVoice("english-default", "en-GB", { default: true }),
    createTestVoice("english-neutral", "en"),
    createTestVoice("chinese-mainland", "zh-CN"),
    createTestVoice("chinese-script", "zh-Hans-CN"),
    createTestVoice("chinese-singapore", "zh-SG"),
    createTestVoice("chinese-neutral", "zh"),
    createTestVoice("traditional", "zh-Hant"),
    createTestVoice("taiwan", "zh-TW"),
    createTestVoice("hong-kong", "zh-HK"),
    createTestVoice("explicit-traditional", "zh-CN-Hant"),
    createTestVoice("unrelated", "fr-FR", { default: true }),
  ];

  it("offers only English and explicitly matching Simplified Chinese locales", () => {
    expect(getMatchingVoices(voices, "en").map((voice) => voice.voiceURI)).toEqual([
      "english-first", "english-default", "english-neutral",
    ]);
    expect(getMatchingVoices(voices, "zh-CN").map((voice) => voice.voiceURI)).toEqual([
      "chinese-mainland", "chinese-script", "chinese-singapore", "chinese-neutral",
    ]);
  });

  it("uses a matching default then first voice, never an unrelated system default", () => {
    expect(selectBrowserVoice(voices, "en", "")?.voiceURI).toBe("english-default");
    expect(selectBrowserVoice(voices, "zh-CN", "")?.voiceURI).toBe("chinese-mainland");
    expect(selectBrowserVoice([], "en", "")).toBeUndefined();
  });

  it("treats explicit voice URIs as opaque and refuses missing or wrong-language selections", () => {
    expect(selectBrowserVoice(voices, "en", "english-first")?.voiceURI).toBe("english-first");
    expect(selectBrowserVoice(voices, "en", " english-first ")).toBeUndefined();
    expect(selectBrowserVoice(voices, "en", "missing")).toBeUndefined();
    expect(selectBrowserVoice(voices, "en", "chinese-mainland")).toBeUndefined();
  });
});

describe("browser voice discovery lifecycle", () => {
  it("detects missing synthesis or utterance support without using any browser service", () => {
    expect(getBrowserSynthesis()).toBeNull();
    vi.stubGlobal("window", { speechSynthesis: createTestSynthesis() });
    expect(getBrowserSynthesis()).toBeNull();
    vi.stubGlobal("window", { SpeechSynthesisUtterance: TestUtterance });
    expect(getBrowserSynthesis()).toBeNull();
    const changed = vi.fn();
    const watcher = watchBrowserVoices(null, changed);
    expect(changed).toHaveBeenLastCalledWith({ status: "unsupported", voices: [] });
    watcher.stop();
    watcher.refresh();
    expect(changed).toHaveBeenCalledOnce();
  });

  it("handles pending, empty, voiceschanged and manual refresh without replacing other listeners", () => {
    vi.useFakeTimers();
    const synthesis = createTestSynthesis();
    const unrelatedListener = vi.fn();
    synthesis.addEventListener("voiceschanged", unrelatedListener);
    const addListener = vi.spyOn(synthesis, "addEventListener");
    const removeListener = vi.spyOn(synthesis, "removeEventListener");
    const changed = vi.fn();
    const watcher = watchBrowserVoices(synthesis, changed);
    expect(changed).toHaveBeenLastCalledWith({ status: "pending", voices: [] });
    vi.advanceTimersByTime(1500);
    expect(changed).toHaveBeenLastCalledWith({ status: "empty", voices: [] });
    const voices = [createTestVoice("loaded", "en-US")];
    synthesis.getVoices.mockReturnValue(voices);
    synthesis.dispatchEvent(new Event("voiceschanged"));
    expect(changed).toHaveBeenLastCalledWith({ status: "ready", voices });
    expect(unrelatedListener).toHaveBeenCalledOnce();
    synthesis.getVoices.mockReturnValue([]);
    watcher.refresh();
    expect(changed).toHaveBeenLastCalledWith({ status: "empty", voices: [] });
    watcher.stop();
    expect(removeListener).toHaveBeenCalledWith("voiceschanged", addListener.mock.calls[0][1]);
    const updateCount = changed.mock.calls.length;
    synthesis.dispatchEvent(new Event("voiceschanged"));
    watcher.refresh();
    vi.runAllTimers();
    expect(changed).toHaveBeenCalledTimes(updateCount);
    expect(unrelatedListener).toHaveBeenCalledTimes(2);
    expect(synthesis.speak).not.toHaveBeenCalled();
    expect(synthesis.cancel).not.toHaveBeenCalled();
  });

  it("clears pending timers and permits a fresh StrictMode-style subscription", () => {
    vi.useFakeTimers();
    const synthesis = createTestSynthesis();
    const oldChanged = vi.fn();
    const oldWatcher = watchBrowserVoices(synthesis, oldChanged);
    oldWatcher.stop();
    const newChanged = vi.fn();
    const newWatcher = watchBrowserVoices(synthesis, newChanged);
    const voices = [createTestVoice("loaded", "zh-CN")];
    synthesis.getVoices.mockReturnValue(voices);
    synthesis.dispatchEvent(new Event("voiceschanged"));
    vi.runAllTimers();
    expect(oldChanged).toHaveBeenCalledOnce();
    expect(newChanged).toHaveBeenCalledTimes(2);
    expect(newChanged).toHaveBeenLastCalledWith({ status: "ready", voices });
    newWatcher.stop();
  });

  it("keeps browser service failures recoverable through manual refresh", () => {
    const synthesis = createTestSynthesis();
    synthesis.getVoices.mockImplementationOnce(() => { throw new Error("unavailable"); });
    const changed = vi.fn();
    const watcher = watchBrowserVoices(synthesis, changed);
    expect(changed).toHaveBeenLastCalledWith({ status: "empty", voices: [] });
    synthesis.getVoices.mockReturnValue([createTestVoice("recovered", "en-US")]);
    watcher.refresh();
    expect(changed.mock.lastCall?.[0].status).toBe("ready");
    watcher.stop();
  });
});
