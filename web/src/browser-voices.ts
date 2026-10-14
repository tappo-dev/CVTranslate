import { useCallback, useEffect, useRef, useState } from "react";
import type { SpeechVoiceLanguage } from "./types";

export interface BrowserVoiceList {
  status: "pending" | "ready" | "empty" | "unsupported";
  voices: SpeechSynthesisVoice[];
}

type VoiceSource = Pick<SpeechSynthesis, "getVoices" | "addEventListener" | "removeEventListener">;

export function getBrowserSynthesis(): SpeechSynthesis | null {
  if (
    typeof window === "undefined" ||
    !window.speechSynthesis ||
    typeof window.SpeechSynthesisUtterance !== "function"
  ) {
    return null;
  }
  return window.speechSynthesis;
}

export function matchesSpeechLanguage(locale: string, language: SpeechVoiceLanguage) {
  const normalized = locale.toLowerCase();
  if (language === "en") return normalized === "en" || normalized.startsWith("en-");
  if (normalized === "zh") return true;
  if (normalized.split("-").includes("hant")) return false;
  return ["zh-cn", "zh-hans", "zh-sg"].some(
    (prefix) => normalized === prefix || normalized.startsWith(`${prefix}-`),
  );
}

export function getMatchingVoices(voices: SpeechSynthesisVoice[], language: SpeechVoiceLanguage) {
  return voices.filter((voice) => matchesSpeechLanguage(voice.lang, language));
}

export function selectBrowserVoice(
  voices: SpeechSynthesisVoice[],
  language: SpeechVoiceLanguage,
  savedVoiceUri: string,
) {
  const matchingVoices = getMatchingVoices(voices, language);
  if (savedVoiceUri) {
    return matchingVoices.find((voice) => voice.voiceURI === savedVoiceUri);
  }
  return matchingVoices.find((voice) => voice.default) ?? matchingVoices[0];
}

export function watchBrowserVoices(
  synthesis: VoiceSource | null,
  onChange: (list: BrowserVoiceList) => void,
) {
  let disposed = false;
  let waitingForInitialList = true;
  let emptyTimer: ReturnType<typeof setTimeout> | undefined;

  function refresh() {
    if (disposed) return;
    if (!synthesis) {
      onChange({ status: "unsupported", voices: [] });
      return;
    }
    let voices: SpeechSynthesisVoice[] = [];
    try {
      voices = synthesis.getVoices();
    } catch {
      // An unavailable browser voice service must not prevent saving other settings.
      waitingForInitialList = false;
    }
    if (voices.length) {
      waitingForInitialList = false;
      clearTimeout(emptyTimer);
    }
    let status: BrowserVoiceList["status"] = "empty";
    if (voices.length) status = "ready";
    else if (waitingForInitialList) status = "pending";
    onChange({ status, voices });
  }

  synthesis?.addEventListener("voiceschanged", refresh);
  refresh();
  if (synthesis && waitingForInitialList) {
    emptyTimer = setTimeout(() => {
      waitingForInitialList = false;
      refresh();
    }, 1500);
  }
  return {
    refresh,
    stop() {
      disposed = true;
      clearTimeout(emptyTimer);
      synthesis?.removeEventListener("voiceschanged", refresh);
    },
  };
}

export function useBrowserVoices() {
  const [list, setList] = useState<BrowserVoiceList>({ status: "pending", voices: [] });
  const watcher = useRef<ReturnType<typeof watchBrowserVoices> | null>(null);
  useEffect(() => {
    const current = watchBrowserVoices(getBrowserSynthesis(), setList);
    watcher.current = current;
    return () => {
      watcher.current = null;
      current.stop();
    };
  }, []);
  const refresh = useCallback(() => watcher.current?.refresh(), []);
  return { list, refresh };
}
