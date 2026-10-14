import { useCallback, useEffect, useRef, useState } from "react";
import { ApiError, requestAudio } from "./api";
import { BrowserSpeechController, getBrowserSpeechDependencies } from "./browser-speech";
import type { SpeechSettings, SpeechVoiceLanguage } from "./types";

export type SpeechTarget = "source" | "translation" | `preview-${SpeechVoiceLanguage}`;
export type SpeechLanguage = "auto" | "en" | "zh-CN";
type SpeechPurpose = "read-aloud" | "preview";
export const speechPreviewTexts: Record<SpeechVoiceLanguage, string> = {
  en: "Hello! This is a preview of the English reading voice.",
  "zh-CN": "你好！这是简体中文朗读声音的试听。",
};
export interface SpeechState {
  activeTarget: SpeechTarget | null;
  status: "stopped" | "loading" | "playing" | "error";
  error: string;
}
export interface SpeechAudio {
  src: string;
  onended: ((event: Event) => void) | null;
  onerror: ((event: Event) => void) | null;
  play: () => Promise<void>;
  pause: () => void;
  load: () => void;
  removeAttribute: (name: string) => void;
}
interface SpeechDependencies {
  fetchAudio: (text: string, language: SpeechLanguage, signal: AbortSignal) => Promise<Blob>;
  createAudio: () => SpeechAudio;
  createUrl: (blob: Blob) => string;
  revokeUrl: (url: string) => void;
}
const stoppedState: SpeechState = { activeTarget: null, status: "stopped", error: "" };

export class SpeechController {
  private generation = 0;
  private controller: AbortController | null = null;
  private audio: SpeechAudio | null = null;
  private audioUrl: string | null = null;
  private state = stoppedState;

  constructor(private dependencies: SpeechDependencies, private onChange: (state: SpeechState) => void) {}

  private release() {
    this.generation += 1;
    this.controller?.abort();
    this.controller = null;
    if (this.audio) {
      this.audio.onended = null;
      this.audio.onerror = null;
      this.audio.pause();
      this.audio.removeAttribute("src");
      this.audio.load();
      this.audio = null;
    }
    if (this.audioUrl) this.dependencies.revokeUrl(this.audioUrl);
    this.audioUrl = null;
  }

  private publish(state: SpeechState) {
    this.state = state;
    this.onChange(state);
  }

  stop() {
    this.release();
    this.publish(stoppedState);
  }

  dispose() {
    this.release();
    this.state = stoppedState;
  }

  async speak(target: SpeechTarget, text: string, language: string) {
    if (this.state.activeTarget === target) {
      this.stop();
      return;
    }
    this.release();
    if (!text.trim() || Array.from(text).length > 20000 || !["auto", "en", "zh-CN"].includes(language)) {
      this.publish({ activeTarget: null, status: "error", error: "朗读需要 1–20,000 字符，语言请选择自动、英语或简体中文。" });
      return;
    }
    const generation = this.generation;
    const controller = new AbortController();
    this.controller = controller;
    this.publish({ activeTarget: target, status: "loading", error: "" });
    const fail = (error: unknown) => {
      if (generation !== this.generation) return;
      this.release();
      const message = error instanceof Error && error.name === "NotAllowedError"
        ? "浏览器阻止了朗读，请允许页面播放音频后再次点击朗读。"
        : error instanceof Error && error.name === "TimeoutError"
          ? "朗读请求超时，请稍后手动重试。不会自动重试。"
          : error instanceof ApiError ? error.message : "朗读失败，请检查网络和音频设备后重试。";
      this.publish({ activeTarget: null, status: "error", error: message });
    };
    try {
      const blob = await this.dependencies.fetchAudio(text, language as SpeechLanguage, controller.signal);
      if (generation !== this.generation) return;
      if (blob.type !== "audio/mpeg" || !blob.size || blob.size > 16 * 1024 * 1024) {
        throw new Error("Invalid audio");
      }
      this.audioUrl = this.dependencies.createUrl(blob);
      const audio = this.dependencies.createAudio();
      this.audio = audio;
      audio.src = this.audioUrl;
      audio.onended = () => {
        if (generation === this.generation) this.stop();
      };
      audio.onerror = () => fail(new Error("Audio playback failed"));
      await audio.play();
      if (generation === this.generation) {
        this.publish({ activeTarget: target, status: "playing", error: "" });
      }
    } catch (error) {
      fail(error);
    }
  }
}

export interface SpeechPlaybackController {
  speak: (target: SpeechTarget, text: string, language: string) => void | Promise<void>;
  stop: () => void;
  dispose: () => void;
}

export function createSpeechController(
  settings: SpeechSettings,
  onChange: (state: SpeechState) => void,
  purpose: SpeechPurpose = "read-aloud",
): SpeechPlaybackController {
  if (settings.engine === "web-speech") {
    return new BrowserSpeechController(getBrowserSpeechDependencies(), { ...settings.webSpeechVoices }, onChange);
  }
  if (settings.engine === "edge") {
    const voices = { ...settings.edgeVoices };
    return new SpeechController({
      fetchAudio: (text, language, signal) => {
        if (purpose === "preview") {
          if (language === "auto") {
            return Promise.reject(new ApiError("请选择英语或简体中文进行试听。", 400, "invalid_language"));
          }
          return requestAudio("/speech/preview", {
            method: "POST", body: { language, voiceId: voices[language] }, signal,
          });
        }
        return requestAudio("/speech", { method: "POST", body: { text, language }, signal });
      },
      createAudio: () => new Audio(),
      createUrl: (blob) => URL.createObjectURL(blob),
      revokeUrl: (url) => URL.revokeObjectURL(url),
    }, onChange);
  }
  let disposed = false;
  return {
    speak() {
      if (!disposed) onChange({ activeTarget: null, status: "error", error: "保存的朗读引擎不受支持，请在应用设置中重新选择引擎并保存。" });
    },
    stop() {
      if (!disposed) onChange(stoppedState);
    },
    dispose() {
      disposed = true;
    },
  };
}

export function useSpeechSynthesis(settings: SpeechSettings, purpose: SpeechPurpose = "read-aloud") {
  const [state, setState] = useState<SpeechState>(stoppedState);
  const reference = useRef<SpeechPlaybackController | null>(null);
  const engine = settings.engine;
  const edgeEnglishVoice = settings.edgeVoices.en;
  const edgeChineseVoice = settings.edgeVoices["zh-CN"];
  const browserEnglishVoice = settings.webSpeechVoices.en;
  const browserChineseVoice = settings.webSpeechVoices["zh-CN"];
  // Refreshing unchanged settings must not dispose an active reading.
  useEffect(() => {
    const controller = createSpeechController({
      engine,
      edgeVoices: { en: edgeEnglishVoice, "zh-CN": edgeChineseVoice },
      webSpeechVoices: { en: browserEnglishVoice, "zh-CN": browserChineseVoice },
    }, setState, purpose);
    reference.current = controller;
    setState(stoppedState);
    return () => {
      reference.current = null;
      controller.dispose();
    };
  }, [engine, edgeEnglishVoice, edgeChineseVoice, browserEnglishVoice, browserChineseVoice, purpose]);
  const stop = useCallback(() => reference.current?.stop(), []);
  const speak = useCallback((target: SpeechTarget, text: string, language: string) => {
    void reference.current?.speak(target, text, language);
  }, []);
  return { ...state, speak, stop };
}
