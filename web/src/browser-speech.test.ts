import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { requestAudio } from "./api";
import { BrowserSpeechController, resolveSpeechLanguage } from "./browser-speech";
import { createSpeechController, speechPreviewTexts } from "./speech";
import { createSpeechEvent, createTestAppState, createTestSynthesis, createTestVoice, TestUtterance } from "./speech-test-fixtures";
import type { SpeechSettings } from "./types";

vi.mock("./api", async (importOriginal) => ({
  ...await importOriginal<typeof import("./api")>(),
  requestAudio: vi.fn(),
}));

const englishVoice = createTestVoice("opaque://english?voice=1", "en-GB");
const chineseVoice = createTestVoice("中文 voice URI", "zh-Hans-CN", { localService: false });

function setup(preferences: SpeechSettings["webSpeechVoices"] = { en: englishVoice.voiceURI, "zh-CN": chineseVoice.voiceURI }) {
  const synthesis = createTestSynthesis([englishVoice, chineseVoice]);
  const createUtterance = vi.fn((text: string) => new TestUtterance(text));
  const changed = vi.fn();
  const controller = new BrowserSpeechController({ synthesis, createUtterance }, preferences, changed);
  return { synthesis, createUtterance, changed, controller };
}

beforeEach(() => {
  vi.mocked(requestAudio).mockReset();
});
afterEach(() => vi.unstubAllGlobals());

describe("browser speech lifecycle", () => {
  it("binds the saved voice URI and its locale, transitioning only on browser events", () => {
    const { controller, synthesis, changed } = setup();
    expect(synthesis.speak).not.toHaveBeenCalled();
    controller.speak("source", "Hello", "en");
    const utterance = synthesis.speak.mock.calls[0][0];
    expect(utterance.text).toBe("Hello");
    expect(utterance.voice).toBe(englishVoice);
    expect(utterance.lang).toBe("en-GB");
    expect(changed).toHaveBeenLastCalledWith({ activeTarget: "source", status: "loading", error: "" });
    utterance.onstart?.(createSpeechEvent(utterance));
    expect(changed).toHaveBeenLastCalledWith({ activeTarget: "source", status: "playing", error: "" });
    utterance.onend?.(createSpeechEvent(utterance));
    expect(changed).toHaveBeenLastCalledWith({ activeTarget: null, status: "stopped", error: "" });
    expect(utterance.onstart).toBeNull();
    expect(utterance.onend).toBeNull();
    expect(utterance.onerror).toBeNull();
    expect(requestAudio).not.toHaveBeenCalled();
  });

  it("selects the current matching default at read time rather than caching the initial voice list", () => {
    const { controller, synthesis } = setup({ en: "", "zh-CN": "" });
    const preferred = createTestVoice("current-default", "en-AU", { default: true });
    synthesis.getVoices.mockReturnValue([chineseVoice, englishVoice, preferred]);
    controller.speak("source", "Hello", "en");
    expect(synthesis.speak.mock.calls[0][0].voice).toBe(preferred);
    controller.dispose();
  });

  it.each(["loading", "playing"])("stops the same button during %s without creating another utterance", (status) => {
    const { controller, synthesis, createUtterance, changed } = setup();
    controller.speak("source", "Hello", "en");
    const utterance = synthesis.speak.mock.calls[0][0];
    if (status === "playing") utterance.onstart?.(createSpeechEvent(utterance));
    controller.speak("source", "Hello", "en");
    expect(synthesis.cancel).toHaveBeenCalledOnce();
    expect(createUtterance).toHaveBeenCalledOnce();
    expect(synthesis.speak).toHaveBeenCalledOnce();
    expect(changed).toHaveBeenLastCalledWith({ activeTarget: null, status: "stopped", error: "" });
  });

  it("cancels on target changes and ignores all stale callbacks even after disposal", () => {
    const { controller, synthesis, changed } = setup();
    controller.speak("source", "Hello", "en");
    const first = synthesis.speak.mock.calls[0][0];
    const oldStart = first.onstart;
    const oldEnd = first.onend;
    const oldError = first.onerror;
    controller.speak("translation", "你好", "zh-CN");
    const second = synthesis.speak.mock.calls[1][0];
    expect(second.voice).toBe(chineseVoice);
    expect(second.lang).toBe("zh-Hans-CN");
    expect(synthesis.cancel).toHaveBeenCalledOnce();
    oldStart?.call(first, createSpeechEvent(first));
    oldEnd?.call(first, createSpeechEvent(first));
    oldError?.call(first, { ...createSpeechEvent(first), error: "voice-unavailable" });
    expect(changed).toHaveBeenLastCalledWith({ activeTarget: "translation", status: "loading", error: "" });
    const lateStart = second.onstart;
    const lateEnd = second.onend;
    const lateError = second.onerror;
    controller.dispose();
    const updateCount = changed.mock.calls.length;
    lateStart?.call(second, createSpeechEvent(second));
    lateEnd?.call(second, createSpeechEvent(second));
    lateError?.call(second, { ...createSpeechEvent(second), error: "not-allowed" });
    controller.stop();
    controller.speak("source", "Hello again", "en");
    controller.dispose();
    expect(changed).toHaveBeenCalledTimes(updateCount);
    expect(synthesis.speak).toHaveBeenCalledTimes(2);
    expect(synthesis.cancel).toHaveBeenCalledTimes(2);
    for (const utterance of [first, second]) {
      expect(utterance.onstart).toBeNull();
      expect(utterance.onend).toBeNull();
      expect(utterance.onerror).toBeNull();
    }
  });

  it("stops an existing utterance before reporting invalid replacement text or language", () => {
    const { controller, synthesis, changed } = setup();
    controller.speak("source", "Hello", "en");
    controller.speak("translation", "", "zh-CN");
    expect(synthesis.cancel).toHaveBeenCalledOnce();
    expect(changed.mock.lastCall?.[0].status).toBe("error");
    controller.speak("source", "Hello", "unsupported");
    controller.speak("source", "😀".repeat(20001), "auto");
    expect(synthesis.speak).toHaveBeenCalledOnce();
  });

  it.each([
    ["not-allowed", "页面音频权限"],
    ["voice-unavailable", "重新选择后试听"],
    ["language-unavailable", "重新选择后试听"],
    ["text-too-long", "缩短文本"],
    ["network", "检查网络"],
  ] as const)("provides safe actionable %s errors without retrying", (error, expectedMessage) => {
    const { controller, synthesis, changed } = setup();
    controller.speak("source", "Hello", "en");
    const utterance = synthesis.speak.mock.calls[0][0];
    utterance.onerror?.({ ...createSpeechEvent(utterance), error });
    expect(changed.mock.lastCall?.[0].error).toContain(expectedMessage);
    expect(synthesis.cancel).toHaveBeenCalledOnce();
    expect(synthesis.speak).toHaveBeenCalledOnce();
    expect(utterance.onerror).toBeNull();
    expect(requestAudio).not.toHaveBeenCalled();
  });

  it("handles synchronous browser refusal without exposing raw error details", () => {
    const { controller, synthesis, changed } = setup();
    synthesis.speak.mockImplementation(() => { throw new DOMException("sensitive browser details", "NotAllowedError"); });
    controller.speak("source", "Hello", "en");
    expect(changed.mock.lastCall?.[0].error).toContain("页面音频权限");
    expect(changed.mock.lastCall?.[0].error).not.toContain("sensitive");
    expect(synthesis.cancel).toHaveBeenCalledOnce();
  });

  it("never falls back for missing explicit voices, empty lists, or unsupported browsers", () => {
    const { controller, synthesis, createUtterance, changed } = setup({ en: "saved-but-missing", "zh-CN": "" });
    controller.speak("source", "Hello", "en");
    expect(changed.mock.lastCall?.[0].error).toContain("不会自动替换声音");
    synthesis.getVoices.mockReturnValue([]);
    controller.speak("translation", "你好", "zh-CN");
    expect(changed.mock.lastCall?.[0].error).toContain("尚未提供此语言");
    const unsupported = new BrowserSpeechController(null, { en: "", "zh-CN": "" }, changed);
    unsupported.speak("source", "Hello", "en");
    expect(changed.mock.lastCall?.[0].error).toContain("不支持 Web Speech API");
    expect(createUtterance).not.toHaveBeenCalled();
    expect(synthesis.speak).not.toHaveBeenCalled();
    expect(requestAudio).not.toHaveBeenCalled();
  });

  it.each([
    ["Hello", "en"], ["你好", "zh-CN"], ["English with 𠀀", "zh-CN"], ["😀", "en"],
  ])("matches the server Unicode Han auto heuristic for %s", (text, language) => {
    expect(resolveSpeechLanguage(text, "auto")).toBe(language);
    const { controller, synthesis } = setup();
    controller.speak("source", text, "auto");
    expect(synthesis.speak.mock.calls[0][0].voice).toBe(language === "en" ? englishVoice : chineseVoice);
    controller.dispose();
  });
});

describe("saved engine dispatch", () => {
  it("routes browser playback without HTTP and cleans up between setup/cleanup lifecycles", async () => {
    const synthesis = createTestSynthesis([englishVoice, chineseVoice]);
    vi.stubGlobal("window", { speechSynthesis: synthesis, SpeechSynthesisUtterance: TestUtterance });
    const settings = createTestAppState().settings.speech;
    settings.engine = "web-speech";
    settings.webSpeechVoices.en = englishVoice.voiceURI;
    const changed = vi.fn();
    const first = createSpeechController(settings, changed);
    expect(synthesis.speak).not.toHaveBeenCalled();
    await first.speak("source", "Hello", "en");
    first.dispose();
    const second = createSpeechController(settings, changed);
    await second.speak("source", "Hello", "en");
    expect(synthesis.speak).toHaveBeenCalledTimes(2);
    expect(synthesis.speak.mock.calls[1][0].voice).toBe(englishVoice);
    expect(requestAudio).not.toHaveBeenCalled();
    second.dispose();
    expect(synthesis.cancel).toHaveBeenCalledTimes(2);
  });

  it("keeps Edge on the shared audio request with only text and language", async () => {
    const synthesis = createTestSynthesis([englishVoice]);
    vi.stubGlobal("window", { speechSynthesis: synthesis, SpeechSynthesisUtterance: TestUtterance });
    vi.mocked(requestAudio).mockRejectedValue(new Error("mock service failure"));
    const controller = createSpeechController(createTestAppState().settings.speech, vi.fn());
    expect(requestAudio).not.toHaveBeenCalled();
    await controller.speak("source", "Hello", "auto");
    expect(requestAudio).toHaveBeenCalledExactlyOnceWith("/speech", {
      method: "POST", body: { text: "Hello", language: "auto" }, signal: expect.any(AbortSignal),
    });
    expect(synthesis.speak).not.toHaveBeenCalled();
    controller.dispose();
  });

  it("reports unknown engine settings without network, browser calls, or fallback", async () => {
    const settings = createTestAppState().settings.speech;
    const changed = vi.fn();
    const controller = createSpeechController({ ...settings, engine: "unknown" as SpeechSettings["engine"] }, changed);
    await controller.speak("source", "Hello", "en");
    expect(changed.mock.lastCall?.[0].error).toContain("引擎不受支持");
    controller.dispose();
    controller.stop();
    await controller.speak("source", "Hello", "en");
    expect(changed).toHaveBeenCalledOnce();
    expect(requestAudio).not.toHaveBeenCalled();
  });
});

describe("draft voice previews", () => {
  it("routes both draft Edge voices to the preview-only endpoint without text or persistence", async () => {
    vi.mocked(requestAudio).mockRejectedValue(new Error("mock service failure"));
    const settings = createTestAppState().settings.speech;
    settings.edgeVoices = { en: "en-US-AriaNeural", "zh-CN": "zh-CN-YunxiNeural" };
    const controller = createSpeechController(settings, vi.fn(), "preview");
    expect(requestAudio).not.toHaveBeenCalled();
    for (const language of ["en", "zh-CN"] as const) {
      await controller.speak(`preview-${language}`, speechPreviewTexts[language], language);
      expect(requestAudio).toHaveBeenLastCalledWith("/speech/preview", {
        method: "POST", body: { language, voiceId: settings.edgeVoices[language] }, signal: expect.any(AbortSignal),
      });
    }
    expect(requestAudio).toHaveBeenCalledTimes(2);
    controller.dispose();
  });

  it("uses browser draft voices for each sample and stops or switches without calling Edge", async () => {
    const synthesis = createTestSynthesis([englishVoice, chineseVoice]);
    vi.stubGlobal("window", { speechSynthesis: synthesis, SpeechSynthesisUtterance: TestUtterance });
    const settings = createTestAppState().settings.speech;
    settings.engine = "web-speech";
    settings.webSpeechVoices = { en: englishVoice.voiceURI, "zh-CN": chineseVoice.voiceURI };
    const controller = createSpeechController(settings, vi.fn(), "preview");
    expect(synthesis.speak).not.toHaveBeenCalled();
    await controller.speak("preview-en", speechPreviewTexts.en, "en");
    expect(synthesis.speak.mock.calls[0][0]).toMatchObject({ text: speechPreviewTexts.en, voice: englishVoice });
    await controller.speak("preview-zh-CN", speechPreviewTexts["zh-CN"], "zh-CN");
    expect(synthesis.cancel).toHaveBeenCalledOnce();
    expect(synthesis.speak.mock.calls[1][0]).toMatchObject({ text: speechPreviewTexts["zh-CN"], voice: chineseVoice });
    await controller.speak("preview-zh-CN", speechPreviewTexts["zh-CN"], "zh-CN");
    expect(synthesis.speak).toHaveBeenCalledTimes(2);
    expect(synthesis.cancel).toHaveBeenCalledTimes(2);
    expect(requestAudio).not.toHaveBeenCalled();
    controller.dispose();
  });
});
