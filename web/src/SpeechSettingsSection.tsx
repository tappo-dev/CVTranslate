import { LoaderCircle, Square, Volume2 } from "lucide-react";
import {
  getMatchingVoices,
  useBrowserVoices,
  type BrowserVoiceList,
} from "./browser-voices";
import { speechPreviewTexts, useSpeechSynthesis } from "./speech";
import type { SpeechSettings, SpeechVoice, SpeechVoiceLanguage } from "./types";
import { Notice } from "./ui";

interface SpeechSettingsProps {
  settings: SpeechSettings;
  edgeVoices: SpeechVoice[];
  onChange: (settings: SpeechSettings) => void;
}

const voiceLanguages: { language: SpeechVoiceLanguage; label: string }[] = [
  { language: "en", label: "英语声音" },
  { language: "zh-CN", label: "简体中文声音" },
];

export function SpeechSettingsFields({
  settings,
  edgeVoices,
  onChange,
  browserVoices,
  refreshBrowserVoices,
  preview,
}: SpeechSettingsProps & {
  browserVoices: BrowserVoiceList;
  refreshBrowserVoices: () => void;
  preview: ReturnType<typeof useSpeechSynthesis>;
}) {
  const browserEngine = settings.engine === "web-speech";
  const knownEngine = browserEngine || settings.engine === "edge";
  function changeSettings(updated: SpeechSettings) {
    preview.stop();
    onChange(updated);
  }
  let browserStatus = "";
  if (browserVoices.status === "pending") {
    browserStatus = "正在加载浏览器声音…";
  } else if (browserVoices.status === "empty") {
    browserStatus = "暂无浏览器声音，请刷新或检查系统声音配置。";
  } else if (browserVoices.status === "unsupported") {
    browserStatus = "当前浏览器不支持 Web Speech API。";
  }

  return (
    <section
      className="card settings-card"
      aria-labelledby="speech-settings-title"
    >
      <div className="section-title">
        <h2 id="speech-settings-title">朗读</h2>
      </div>
      <div className="settings-fields">
        <div className="settings-row">
          <label htmlFor="speech-engine">朗读引擎</label>
          <div className="settings-control">
            <select
              name="speechEngine"
              id="speech-engine"
              value={settings.engine}
              onChange={(event) => {
                const engine = event.target.value;
                if (engine === "edge" || engine === "web-speech") {
                  changeSettings({ ...settings, engine });
                }
              }}
            >
              {!knownEngine && (
                <option value={settings.engine}>不受支持的已保存引擎</option>
              )}
              <option value="edge">Edge 在线朗读（Microsoft）</option>
              <option value="web-speech">Web Speech API（浏览器）</option>
            </select>
          </div>
        </div>
        {knownEngine &&
          voiceLanguages.map(({ language, label }) => {
            const preferencesKey = browserEngine
              ? "webSpeechVoices"
              : "edgeVoices";
            const selectedVoice = settings[preferencesKey][language];
            const options = browserEngine
              ? getMatchingVoices(browserVoices.voices, language).map(
                  (voice) => ({
                    value: voice.voiceURI,
                    label: `${voice.name} · ${voice.lang} · ${voice.localService ? "本机" : "在线"}`,
                  }),
                )
              : edgeVoices
                  .filter((voice) => voice.language === language)
                  .map((voice) => ({
                    value: voice.id,
                    label: voice.name,
                  }));
            const usesBrowserDefault = browserEngine && selectedVoice === "";
            const unavailable =
              !usesBrowserDefault &&
              !options.some((option) => option.value === selectedVoice);
            const noMatchingVoices =
              browserEngine &&
              browserVoices.status === "ready" &&
              options.length === 0;
            const controlId = `speech-voice-${language}`;
            const helpId = `speech-voice-${language}-help`;
            const active = preview.activeTarget === `preview-${language}`;
            const loading = active && preview.status === "loading";
            const previewUnavailable =
              unavailable ||
              (browserEngine &&
                (browserVoices.status !== "ready" || options.length === 0));
            const buttonLabel = loading
              ? "取消加载"
              : active
                ? "停止试听"
                : "试听";
            return (
              <div className="settings-row speech-voice-field" key={language}>
                <label htmlFor={controlId}>{label}</label>
                <div className="settings-control">
                  <div className="speech-voice-controls">
                    <select
                      name={`speechVoice-${language}`}
                      id={controlId}
                      value={selectedVoice}
                      required={!browserEngine}
                      aria-describedby={
                        unavailable || noMatchingVoices ? helpId : undefined
                      }
                      onChange={(event) =>
                        changeSettings({
                          ...settings,
                          [preferencesKey]: {
                            ...settings[preferencesKey],
                            [language]: event.target.value,
                          },
                        })
                      }
                    >
                      {browserEngine && <option value="">浏览器默认</option>}
                      {unavailable && (
                        <option value={selectedVoice}>
                          不可用：{selectedVoice || "未设置"}
                        </option>
                      )}
                      {options.map((option) => (
                        <option key={option.value} value={option.value}>
                          {option.label}
                        </option>
                      ))}
                    </select>
                    <button
                      type="button"
                      name={`speechPreview-${language}`}
                      className={
                        active
                          ? "speech-preview-button speaking"
                          : "speech-preview-button"
                      }
                      aria-label={`${buttonLabel}：${label}`}
                      aria-pressed={active}
                      aria-describedby={
                        unavailable || noMatchingVoices ? helpId : undefined
                      }
                      disabled={!active && previewUnavailable}
                      onClick={() =>
                        preview.speak(
                          `preview-${language}`,
                          speechPreviewTexts[language],
                          language,
                        )
                      }
                    >
                      {loading ? (
                        <LoaderCircle className="spin" size={16} />
                      ) : active ? (
                        <Square size={13} />
                      ) : (
                        <Volume2 size={16} />
                      )}
                      {buttonLabel}
                    </button>
                  </div>
                  {(unavailable || noMatchingVoices) && (
                    <small id={helpId} role="status">
                      {unavailable
                        ? "所选声音不可用，已保留选择。"
                        : "此语言暂无可用声音。"}
                    </small>
                  )}
                </div>
              </div>
            );
          })}
      </div>
      <Notice error>{preview.error && <>试听失败：{preview.error}</>}</Notice>
      {!knownEngine && (
        <Notice error>已保存的引擎不受支持，请重新选择并保存后朗读。</Notice>
      )}
      <div className="settings-speech-feedback">
        {browserEngine && (
          <>
            <Notice>{browserStatus}</Notice>
            <button
              type="button"
              onClick={() => {
                preview.stop();
                refreshBrowserVoices();
              }}
              disabled={browserVoices.status === "unsupported"}
            >
              刷新浏览器声音
            </button>
            <p className="help-text">
              在线声音可能将文本发送至声音服务商；本机/在线标记由浏览器提供。
            </p>
          </>
        )}
        {settings.engine === "edge" && (
          <p className="help-text">
            朗读发送至 Microsoft；试听使用固定示例，音频不保存。
          </p>
        )}
      </div>
    </section>
  );
}

export function SpeechSettingsSection(props: SpeechSettingsProps) {
  const { list, refresh } = useBrowserVoices();
  const preview = useSpeechSynthesis(props.settings, "preview");
  return (
    <SpeechSettingsFields
      {...props}
      browserVoices={list}
      refreshBrowserVoices={refresh}
      preview={preview}
    />
  );
}
