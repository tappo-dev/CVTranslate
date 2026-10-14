import { vi } from "vitest";
import type { AppState } from "./types";

export function createTestVoice(voiceURI: string, lang: string, overrides: Partial<SpeechSynthesisVoice> = {}): SpeechSynthesisVoice {
  return { voiceURI, lang, name: voiceURI, default: false, localService: true, ...overrides };
}

export class TestUtterance extends EventTarget implements SpeechSynthesisUtterance {
  lang = "";
  voice: SpeechSynthesisVoice | null = null;
  volume = 1;
  rate = 1;
  pitch = 1;
  onstart: SpeechSynthesisUtterance["onstart"] = null;
  onend: SpeechSynthesisUtterance["onend"] = null;
  onerror: SpeechSynthesisUtterance["onerror"] = null;
  onboundary: SpeechSynthesisUtterance["onboundary"] = null;
  onmark: SpeechSynthesisUtterance["onmark"] = null;
  onpause: SpeechSynthesisUtterance["onpause"] = null;
  onresume: SpeechSynthesisUtterance["onresume"] = null;

  constructor(public text: string) {
    super();
  }
}

export function createSpeechEvent(utterance: SpeechSynthesisUtterance): SpeechSynthesisEvent {
  return Object.assign(new Event("speech"), { utterance, charIndex: 0, charLength: 0, elapsedTime: 0, name: "" });
}

export function createTestSynthesis(voices: SpeechSynthesisVoice[] = []) {
  return Object.assign(new EventTarget(), {
    getVoices: vi.fn(() => voices),
    speak: vi.fn<(utterance: SpeechSynthesisUtterance) => void>(),
    cancel: vi.fn(),
  });
}

export function createTestAppState(): AppState {
  return {
    providers: [],
    providerTypes: [],
    prompts: [],
    settings: {
      defaultProviderId: "",
      defaultPromptId: "",
      sourceLanguage: "auto",
      targetLanguage: "zh-CN",
      port: 19327,
      hotkeys: { openUi: "" },
      speech: {
        engine: "edge",
        edgeVoices: { en: "en-US-AndrewMultilingualNeural", "zh-CN": "zh-CN-XiaoxiaoMultilingualNeural" },
        webSpeechVoices: { en: "", "zh-CN": "" },
      },
    },
    speechVoices: [
      { id: "en-US-AndrewMultilingualNeural", name: "Andrew", language: "en" },
      { id: "zh-CN-XiaoxiaoMultilingualNeural", name: "Xiaoxiao", language: "zh-CN" },
    ],
    languages: [{ code: "en", name: "English" }, { code: "zh-CN", name: "Chinese" }],
    platform: { os: "windows", hotkeysAvailable: true, pasteAvailable: true, message: "" },
    version: "test",
    development: true,
  };
}
