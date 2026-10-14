import { getBrowserSynthesis, selectBrowserVoice } from "./browser-voices";
import type { SpeechState, SpeechTarget } from "./speech";
import type { SpeechSettings, SpeechVoiceLanguage } from "./types";

export interface BrowserSpeechDependencies {
  synthesis: Pick<SpeechSynthesis, "getVoices" | "speak" | "cancel">;
  createUtterance: (text: string) => SpeechSynthesisUtterance;
}

export function getBrowserSpeechDependencies(): BrowserSpeechDependencies | null {
  const synthesis = getBrowserSynthesis();
  if (!synthesis) return null;
  return {
    synthesis,
    createUtterance: (text) => new window.SpeechSynthesisUtterance(text),
  };
}

export function resolveSpeechLanguage(text: string, language: string): SpeechVoiceLanguage | null {
  if (language === "auto") return /\p{Script=Han}/u.test(text) ? "zh-CN" : "en";
  if (language === "en" || language === "zh-CN") return language;
  return null;
}

function browserSpeechError(code: string) {
  switch (code) {
    case "not-allowed":
      return "浏览器阻止了朗读，请检查页面音频权限后再次点击朗读。";
    case "voice-unavailable":
    case "language-unavailable":
      return "所选声音或语言当前不可用，请在应用设置中刷新声音列表，重新选择后试听；工作台朗读请先保存设置。";
    case "text-too-long":
      return "此浏览器声音无法朗读这么长的文本，请缩短文本后重试（应用上限为 20,000 字符）。";
    case "audio-busy":
    case "audio-hardware":
      return "浏览器无法使用音频设备，请检查设备和其他音频应用后手动重试。";
    case "network":
      return "此浏览器声音的网络服务不可用，请检查网络，或在设置中选择其他声音后试听；工作台朗读请先保存设置。";
    default:
      return "浏览器朗读未完成，请检查声音和音频设置后手动重试。不会自动切换引擎。";
  }
}

const stoppedState: SpeechState = { activeTarget: null, status: "stopped", error: "" };

export class BrowserSpeechController {
  private generation = 0;
  private disposed = false;
  private utterance: SpeechSynthesisUtterance | null = null;
  private state = stoppedState;

  constructor(
    private dependencies: BrowserSpeechDependencies | null,
    private voices: SpeechSettings["webSpeechVoices"],
    private onChange: (state: SpeechState) => void,
  ) {}

  private release() {
    this.generation += 1;
    if (!this.utterance) return;
    this.utterance.onstart = null;
    this.utterance.onend = null;
    this.utterance.onerror = null;
    this.utterance = null;
    this.dependencies?.synthesis.cancel();
  }

  private publish(state: SpeechState) {
    if (this.disposed) return;
    this.state = state;
    this.onChange(state);
  }

  stop() {
    if (this.disposed) return;
    this.release();
    this.publish(stoppedState);
  }

  dispose() {
    if (this.disposed) return;
    this.disposed = true;
    this.release();
    this.state = stoppedState;
  }

  speak(target: SpeechTarget, text: string, language: string) {
    if (this.disposed) return;
    if (this.state.activeTarget === target) {
      this.stop();
      return;
    }
    this.release();
    const resolvedLanguage = resolveSpeechLanguage(text, language);
    if (!text.trim() || Array.from(text).length > 20000 || !resolvedLanguage) {
      this.publish({ activeTarget: null, status: "error", error: "朗读需要 1–20,000 字符，语言请选择自动、英语或简体中文。" });
      return;
    }
    if (!this.dependencies) {
      this.publish({ activeTarget: null, status: "error", error: "当前浏览器不支持 Web Speech API。请使用支持的桌面浏览器，或在应用设置中选择 Edge 并保存。" });
      return;
    }
    const generation = this.generation;
    const isCurrent = () => !this.disposed && generation === this.generation;
    const fail = (message: string) => {
      if (!isCurrent()) return;
      this.release();
      this.publish({ activeTarget: null, status: "error", error: message });
    };
    try {
      const selectedVoiceUri = this.voices[resolvedLanguage];
      const voice = selectBrowserVoice(this.dependencies.synthesis.getVoices(), resolvedLanguage, selectedVoiceUri);
      if (!voice) {
        const message = selectedVoiceUri
          ? "所选浏览器声音当前不可用。请在应用设置中刷新列表，重新选择声音或浏览器默认后试听；工作台朗读请先保存设置，不会自动替换声音。"
          : "浏览器尚未提供此语言的声音。请在应用设置中刷新列表，检查浏览器或系统声音配置后重试。";
        fail(message);
        return;
      }
      const utterance = this.dependencies.createUtterance(text);
      this.utterance = utterance;
      utterance.voice = voice;
      utterance.lang = voice.lang;
      utterance.onstart = () => {
        if (isCurrent()) this.publish({ activeTarget: target, status: "playing", error: "" });
      };
      utterance.onend = () => {
        if (isCurrent()) this.stop();
      };
      utterance.onerror = (event) => fail(browserSpeechError(event.error));
      this.publish({ activeTarget: target, status: "loading", error: "" });
      this.dependencies.synthesis.speak(utterance);
    } catch (error) {
      const code = error instanceof Error && error.name === "NotAllowedError" ? "not-allowed" : "synthesis-failed";
      fail(browserSpeechError(code));
    }
  }
}
