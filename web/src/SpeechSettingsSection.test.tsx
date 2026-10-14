import {
  Children,
  isValidElement,
  type ButtonHTMLAttributes,
  type ChangeEvent,
  type MouseEvent,
  type ReactNode,
  type SelectHTMLAttributes,
} from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { afterEach, describe, expect, it, vi } from "vitest";
import { SpeechSettingsFields } from "./SpeechSettingsSection";
import { createTestAppState, createTestVoice } from "./speech-test-fixtures";
import type { BrowserVoiceList } from "./browser-voices";
import type { SpeechSettings } from "./types";
import { speechPreviewTexts, type SpeechState } from "./speech";

function findSelect(
  tree: ReactNode,
  name: string,
): SelectHTMLAttributes<HTMLSelectElement> | undefined {
  for (const child of Children.toArray(tree)) {
    if (!isValidElement<{ name?: string; children?: ReactNode }>(child))
      continue;
    if (child.type === "select" && child.props.name === name)
      return child.props;
    const nested = findSelect(child.props.children, name);
    if (nested) return nested;
  }
  return undefined;
}

function findButton(
  tree: ReactNode,
  name: string,
): ButtonHTMLAttributes<HTMLButtonElement> | undefined {
  for (const child of Children.toArray(tree)) {
    if (!isValidElement<{ name?: string; children?: ReactNode }>(child))
      continue;
    if (child.type === "button" && child.props.name === name)
      return child.props;
    const nested = findButton(child.props.children, name);
    if (nested) return nested;
  }
  return undefined;
}

const englishVoice = createTestVoice(
  "opaque://english?key=one&name=two",
  "en-GB",
  { name: "English voice" },
);
const chineseVoice = createTestVoice("opaque chinese URI", "zh-Hans", {
  name: "Chinese voice",
  localService: false,
});
const voices: BrowserVoiceList = {
  status: "ready",
  voices: [englishVoice, chineseVoice],
};

function setup(initial?: SpeechSettings) {
  const state = createTestAppState();
  let settings = initial ?? state.settings.speech;
  const onChange = vi.fn((updated: SpeechSettings) => {
    settings = updated;
  });
  const refresh = vi.fn();
  const playbackState: SpeechState = {
    activeTarget: null,
    status: "stopped",
    error: "",
  };
  const preview = { ...playbackState, speak: vi.fn(), stop: vi.fn() };
  const render = (browserVoices = voices) =>
    SpeechSettingsFields({
      settings,
      edgeVoices: state.speechVoices,
      onChange,
      browserVoices,
      refreshBrowserVoices: refresh,
      preview,
    });
  function select(name: string, value: string) {
    const control = findSelect(render(), name);
    expect(control?.onChange).toBeTypeOf("function");
    control?.onChange?.({
      target: { value },
    } as ChangeEvent<HTMLSelectElement>);
  }
  return {
    render,
    select,
    onChange,
    refresh,
    preview,
    getSettings: () => settings,
  };
}

afterEach(() => vi.unstubAllGlobals());

describe("speech settings form binding", () => {
  it("binds catalog voices by language and retains both engines' independent draft selections", () => {
    const saved = createTestAppState().settings.speech;
    const original = structuredClone(saved);
    const { render, select, getSettings } = setup(saved);
    expect(findSelect(render(), "speechVoice-en")?.value).toBe(
      saved.edgeVoices.en,
    );
    expect(findSelect(render(), "speechVoice-zh-CN")?.value).toBe(
      saved.edgeVoices["zh-CN"],
    );
    const markup = renderToStaticMarkup(render());
    const englishSelect = markup.match(
      /<select name="speechVoice-en"[^>]*>(.*?)<\/select>/,
    )?.[1];
    expect(englishSelect).toContain("Andrew");
    expect(englishSelect).not.toContain("Xiaoxiao");
    select("speechEngine", "web-speech");
    select("speechVoice-en", englishVoice.voiceURI);
    select("speechVoice-zh-CN", chineseVoice.voiceURI);
    select("speechEngine", "edge");
    expect(getSettings().edgeVoices).toEqual(original.edgeVoices);
    expect(findSelect(render(), "speechVoice-en")?.value).toBe(
      original.edgeVoices.en,
    );
    select("speechEngine", "web-speech");
    expect(findSelect(render(), "speechVoice-en")?.value).toBe(
      englishVoice.voiceURI,
    );
    expect(findSelect(render(), "speechVoice-zh-CN")?.value).toBe(
      chineseVoice.voiceURI,
    );
    expect(saved).toEqual(original);
  });

  it("renders explicit browser-default choices, opaque URIs, locale filtering and privacy labels", () => {
    const { render, select } = setup();
    select("speechEngine", "web-speech");
    const markup = renderToStaticMarkup(
      render({
        ...voices,
        voices: [
          ...voices.voices,
          createTestVoice("exclude-traditional", "zh-TW"),
          createTestVoice("exclude-french", "fr-FR"),
        ],
      }),
    );
    expect(markup.match(/<option value="" selected="">/g)).toHaveLength(2);
    expect(markup).toContain("浏览器默认");
    expect(markup).toContain("opaque://english?key=one&amp;name=two");
    expect(markup).toContain("本机");
    expect(markup).toContain("在线");
    expect(markup).toContain("可能将文本发送至声音服务商");
    expect(markup).not.toContain("exclude-traditional");
    expect(markup).not.toContain("exclude-french");
    expect(markup).toContain('type="button"');
    expect(markup).not.toContain('type="submit"');
  });

  it.each(["pending", "empty", "unsupported"] as const)(
    "preserves unavailable saved voices while reporting %s",
    (status) => {
      const settings = createTestAppState().settings.speech;
      settings.engine = "web-speech";
      settings.webSpeechVoices.en = "saved unavailable URI";
      const { render, onChange, getSettings } = setup(settings);
      const markup = renderToStaticMarkup(render({ status, voices: [] }));
      expect(markup).toContain(
        '<option value="saved unavailable URI" selected="">',
      );
      expect(markup).toContain("已保留选择");
      expect(getSettings().webSpeechVoices.en).toBe("saved unavailable URI");
      expect(onChange).not.toHaveBeenCalled();
      if (status === "pending") expect(markup).toContain("正在加载浏览器声音");
      if (status === "empty") expect(markup).toContain("暂无浏览器声音");
      if (status === "unsupported")
        expect(markup).toContain("当前浏览器不支持 Web Speech API");
    },
  );

  it("reports a ready list with no matching language instead of inventing voices", () => {
    const { render, select } = setup();
    select("speechEngine", "web-speech");
    const markup = renderToStaticMarkup(
      render({ status: "ready", voices: [englishVoice] }),
    );
    expect(markup).toContain("此语言暂无可用声音");
    const chineseSelect = markup.match(
      /<select name="speechVoice-zh-CN"[^>]*>(.*?)<\/select>/,
    )?.[1];
    expect(chineseSelect).not.toContain(englishVoice.voiceURI);
    expect(chineseSelect?.match(/<option /g)).toHaveLength(1);
  });

  it("previews both language samples only on explicit clicks without changing the settings", () => {
    const { render, preview, onChange } = setup();
    const tree = render();
    expect(preview.speak).not.toHaveBeenCalled();
    for (const language of ["en", "zh-CN"] as const) {
      const button = findButton(tree, `speechPreview-${language}`);
      expect(button?.type).toBe("button");
      expect(button?.disabled).toBe(false);
      button?.onClick?.({} as MouseEvent<HTMLButtonElement>);
      expect(preview.speak).toHaveBeenLastCalledWith(
        `preview-${language}`,
        speechPreviewTexts[language],
        language,
      );
    }
    expect(onChange).not.toHaveBeenCalled();
  });

  it.each(["loading", "playing"] as const)(
    "keeps a %s preview cancellable even if its voice disappears",
    (status) => {
      const { render, select, preview } = setup();
      select("speechEngine", "web-speech");
      preview.activeTarget = "preview-en";
      preview.status = status;
      const tree = render({ status: "empty", voices: [] });
      const button = findButton(tree, "speechPreview-en");
      expect(button?.disabled).toBe(false);
      expect(button?.["aria-pressed"]).toBe(true);
      expect(button?.["aria-label"]).toContain(
        status === "loading" ? "取消加载" : "停止试听",
      );
      expect(findButton(tree, "speechPreview-zh-CN")?.disabled).toBe(true);
      button?.onClick?.({} as MouseEvent<HTMLButtonElement>);
      expect(preview.speak).toHaveBeenCalledExactlyOnceWith(
        "preview-en",
        speechPreviewTexts.en,
        "en",
      );
    },
  );

  it("stops previews on voice/engine changes and disables unavailable draft voices", () => {
    const { render, select, preview } = setup();
    select("speechEngine", "web-speech");
    select("speechVoice-en", "unavailable URI");
    expect(preview.stop).toHaveBeenCalledTimes(2);
    expect(preview.speak).not.toHaveBeenCalled();
    expect(findButton(render(), "speechPreview-en")?.disabled).toBe(true);
    expect(findButton(render(), "speechPreview-zh-CN")?.disabled).toBe(false);
    for (const status of ["pending", "empty", "unsupported"] as const) {
      expect(
        findButton(render({ status, voices: [] }), "speechPreview-zh-CN")
          ?.disabled,
      ).toBe(true);
    }
    preview.error = "Playback was blocked.";
    const markup = renderToStaticMarkup(render());
    expect(markup).toContain('role="alert"');
    expect(markup).toContain("试听失败：Playback was blocked.");
  });
});
