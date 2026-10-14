import { useEffect, useRef, useState, type ReactNode } from "react";
import * as Tabs from "@radix-ui/react-tabs";
import {
  CheckCircle2,
  ChevronRight,
  CircleAlert,
  Keyboard,
  RefreshCw,
  Save,
  Trash2,
} from "lucide-react";
import { request } from "./api";
import { useFeedback } from "./feedback";
import type { UpdateState } from "./App";
import type { AppState, Settings } from "./types";
import { Notice, useAction } from "./ui";
import { recordHotkey, validateHotkeys } from "./validation";
import { LanguageOptions } from "./LanguageOptions";
import {
  getShortcutActionLabel,
  getShortcutStageLabel,
  watchPlatformStatus,
} from "./shortcut-status";
import { SpeechSettingsSection } from "./SpeechSettingsSection";
import { VersionSection } from "./VersionSection";

function copySettings(settings: Settings): Settings {
  return {
    ...settings,
    hotkeys: { ...settings.hotkeys },
    speech: {
      ...settings.speech,
      edgeVoices: { ...settings.speech.edgeVoices },
      webSpeechVoices: { ...settings.speech.webSpeechVoices },
    },
  };
}

function ShortcutKeys({ value }: { value: string }) {
  return (
    <span className="shortcut-keys" aria-hidden="true">
      {value.split("+").map((keyName) => (
        <kbd key={keyName}>{keyName}</kbd>
      ))}
    </span>
  );
}

function ClipboardGesture({ platform, count }: { platform: string; count: number }) {
  const modifier = platform === "darwin" ? "Command" : "Ctrl";
  return (
    <span className="hotkey-summary">
      {platform === "windows" || platform === "darwin" ? (
        <>
          <span className="sr-only">
            {modifier}+C 连按 {count} 次
          </span>
          <ShortcutKeys value={`${modifier}+C`} />
          <span aria-hidden="true">×{count}</span>
        </>
      ) : (
        "当前平台不可用"
      )}
    </span>
  );
}

function AvailabilityBadge({
  available,
  children,
}: {
  available: boolean;
  children: ReactNode;
}) {
  const Icon = available ? CheckCircle2 : CircleAlert;
  return (
    <span className={`badge ${available ? "succeeded" : "unavailable"}`}>
      <Icon size={14} aria-hidden="true" />
      {children}
    </span>
  );
}

function HotkeyRecorder({
  label,
  description,
  value,
  onChange,
  isMac,
  disabled,
}: {
  label: string;
  description?: string;
  value: string;
  onChange: (value: string) => void;
  isMac: boolean;
  disabled: boolean;
}) {
  const [recording, setRecording] = useState(false);
  const [error, setError] = useState("");
  return (
    <div className="hotkey-row">
      <div>
        <strong>{label}</strong>
        {description && <p>{description}</p>}
      </div>
      <div className="hotkey-input">
        <button
          type="button"
          className={recording ? "hotkey recording" : "hotkey"}
          disabled={disabled}
          aria-label={
            recording
              ? `${label}：请按下快捷键`
              : `${label}：${value || "已禁用"}，点击录制`
          }
          aria-pressed={recording}
          onClick={() => {
            setRecording(true);
            setError("");
          }}
          onBlur={() => setRecording(false)}
          onKeyDown={(event) => {
            if (!recording) return;
            if (event.key === "Tab") {
              setRecording(false);
              return;
            }
            event.preventDefault();
            event.stopPropagation();
            const result = recordHotkey(event, isMac);
            if (result.cancel) {
              setRecording(false);
              setError("");
            } else if (result.value !== undefined) {
              onChange(result.value);
              setRecording(false);
              setError("");
            } else if (result.error) setError(result.error);
          }}
        >
          <Keyboard size={16} aria-hidden="true" />
          {recording ? (
            "请按下快捷键…"
          ) : value ? (
            <ShortcutKeys value={value} />
          ) : (
            "未设置 / 已禁用"
          )}
        </button>
        {recording && <small>Esc 取消 · Delete 禁用 · Tab 离开</small>}
        {error && (
          <span className="invalid" role="status">
            {error}
          </span>
        )}
      </div>
    </div>
  );
}

export function SettingsPage({
  state,
  updateState,
}: {
  state: AppState;
  updateState: UpdateState;
}) {
  const [draft, setDraft] = useState<Settings>(() =>
    copySettings(state.settings),
  );
  const [tab, setTab] = useState("translation");
  const tabList = useRef<HTMLDivElement>(null);
  const saveBar = useRef<HTMLDivElement>(null);
  // Props may still contain an older snapshot after saving; polling owns this local status.
  const [platform, setPlatform] = useState(state.platform);
  const [platformError, setPlatformError] = useState("");
  const platformWatcher = useRef<ReturnType<typeof watchPlatformStatus> | null>(
    null,
  );
  useEffect(() => {
    const watcher = watchPlatformStatus(setPlatform, setPlatformError);
    platformWatcher.current = watcher;
    return () => {
      watcher.stop();
      platformWatcher.current = null;
    };
  }, []);
  const shortcut = platform.shortcut;
  const action = useAction();
  const historyAction = useAction();
  const { confirm } = useFeedback();
  const dirty = JSON.stringify(draft) !== JSON.stringify(state.settings);
  const defaultProvider = state.providers.find(
    (provider) => provider.id === draft.defaultProviderId,
  );
  const supportsPrompts =
    state.providerTypes.find(
      (capabilities) => capabilities.type === defaultProvider?.type,
    )?.supportsPrompts ?? false;
  function restoreTabFocus() {
    if (saveBar.current?.contains(document.activeElement)) {
      tabList.current?.querySelector<HTMLElement>('[data-state="active"]')?.focus();
    }
  }
  function save() {
    if (
      !Number.isInteger(draft.port) ||
      draft.port < 1024 ||
      draft.port > 65535
    ) {
      setTab("service");
      action.setError("端口范围为 1024–65535。");
      return;
    }
    if (
      supportsPrompts &&
      !state.prompts.some((prompt) => prompt.id === draft.defaultPromptId)
    ) {
      setTab("translation");
      action.setError("请选择默认提示词。");
      return;
    }
    const validationError = validateHotkeys(
      Object.values(draft.hotkeys),
      platform.os,
    );
    if (validationError) {
      setTab("hotkeys");
      action.setError(validationError);
      return;
    }
    void action.run(
      async (signal) => {
        const settings = await request<Settings>("/settings", {
          method: "PUT",
          body: draft,
          signal,
        });
        if (!signal.aborted) {
          restoreTabFocus();
          updateState({ ...state, settings });
          setDraft(copySettings(settings));
        }
      },
      draft.port !== state.settings.port
        ? "设置已保存，端口重启后生效。"
        : "设置已保存",
    );
  }

  async function clearHistory() {
    if (
      !(await confirm({
        title: "清空翻译历史？",
        description: "永久删除，无法撤销。",
        confirmLabel: "清空历史",
        danger: true,
      }))
    )
      return;
    void historyAction.run(async (signal) => {
      await request("/history", { method: "DELETE", signal });
    }, "历史记录已清空。");
  }

  return (
    <div className="settings-container">
      <Notice error>{action.error}</Notice>
      <Tabs.Root value={tab} onValueChange={setTab}>
        <Tabs.List ref={tabList} className="settings-tabs" aria-label="设置分类">
          <Tabs.Trigger value="translation">翻译</Tabs.Trigger>
          <Tabs.Trigger value="speech">朗读</Tabs.Trigger>
          <Tabs.Trigger value="hotkeys">快捷键</Tabs.Trigger>
          <Tabs.Trigger value="service">本地服务</Tabs.Trigger>
          <Tabs.Trigger value="about">版本</Tabs.Trigger>
        </Tabs.List>
        <form
          onSubmit={(event) => {
            event.preventDefault();
            save();
          }}
        >
          <fieldset disabled={action.busy}>
            <Tabs.Content value="translation">
              <section className="card settings-card">
                <div className="section-title">
                  <h2>翻译默认值</h2>
                </div>
                <div className="settings-fields">
                  <div className="settings-row">
                    <label htmlFor="default-provider">默认服务</label>
                    <div className="settings-control">
                      <select
                        id="default-provider"
                        value={draft.defaultProviderId}
                        onChange={(event) =>
                          setDraft({
                            ...draft,
                            defaultProviderId: event.target.value,
                          })
                        }
                      >
                        <option value="">未设置</option>
                        {state.providers
                          .filter((provider) => provider.enabled)
                          .map((provider) => (
                            <option key={provider.id} value={provider.id}>
                              {provider.name}
                            </option>
                          ))}
                      </select>
                    </div>
                  </div>
                  <div className="settings-row">
                    <label htmlFor="default-prompt">默认提示词</label>
                    <div className="settings-control">
                      <select
                        id="default-prompt"
                        required={supportsPrompts}
                        disabled={!supportsPrompts}
                        aria-describedby={
                          !supportsPrompts ? "default-prompt-help" : undefined
                        }
                        value={draft.defaultPromptId}
                        onChange={(event) =>
                          setDraft({
                            ...draft,
                            defaultPromptId: event.target.value,
                          })
                        }
                      >
                        <option value="" disabled>
                          请选择默认提示词
                        </option>
                        {state.prompts.map((prompt) => (
                          <option key={prompt.id} value={prompt.id}>
                            {prompt.name}
                          </option>
                        ))}
                      </select>
                      {!supportsPrompts && (
                        <small id="default-prompt-help">
                          {defaultProvider
                            ? "当前服务不使用提示词"
                            : "请先选择默认服务"}
                        </small>
                      )}
                    </div>
                  </div>
                  <div className="settings-row">
                    <label htmlFor="default-source-language">默认源语言</label>
                    <div className="settings-control">
                      <select
                        id="default-source-language"
                        value={draft.sourceLanguage}
                        onChange={(event) =>
                          setDraft({
                            ...draft,
                            sourceLanguage: event.target.value,
                          })
                        }
                      >
                        <LanguageOptions state={state} source />
                      </select>
                    </div>
                  </div>
                  <div className="settings-row">
                    <label htmlFor="default-target-language">默认目标语言</label>
                    <div className="settings-control">
                      <select
                        id="default-target-language"
                        value={draft.targetLanguage}
                        onChange={(event) =>
                          setDraft({
                            ...draft,
                            targetLanguage: event.target.value,
                          })
                        }
                      >
                        <LanguageOptions state={state} automaticTarget />
                      </select>
                    </div>
                  </div>
                </div>
                <section
                  className="settings-data"
                  aria-labelledby="settings-data-title"
                >
                  <div>
                    <h3 id="settings-data-title">数据</h3>
                    <p>本机翻译历史</p>
                  </div>
                  <button
                    type="button"
                    className="danger"
                    disabled={historyAction.busy}
                    aria-busy={historyAction.busy}
                    onClick={() => void clearHistory()}
                  >
                    <Trash2 size={16} />
                    清空历史
                  </button>
                </section>
                <Notice error>{historyAction.error}</Notice>
              </section>
            </Tabs.Content>
            <Tabs.Content value="speech">
              <SpeechSettingsSection
                settings={draft.speech}
                edgeVoices={state.speechVoices}
                onChange={(speech) =>
                  setDraft((current) => ({ ...current, speech }))
                }
              />
            </Tabs.Content>
            <Tabs.Content value="hotkeys">
              <section className="card settings-card">
                <div className="section-title">
                  <h2>全局快捷键</h2>
                </div>
                {!platform.hotkeysAvailable && (
                  <Notice>
                    当前会话的全局快捷键不可用。{platform.message}
                  </Notice>
                )}
                <div className="settings-shortcuts">
                  <section aria-label="剪贴板翻译">
                    <h3>剪贴板翻译</h3>
                    {platform.os !== "windows" && platform.os !== "darwin" && (
                      <p className="help-text">
                        连续复制触发仅支持 Windows 和 macOS。
                      </p>
                    )}
                    {platform.os === "darwin" && (
                      <p className="help-text">
                        需手动授予辅助功能和输入监控权限，并重启应用。
                      </p>
                    )}
                    <div className="hotkey-row">
                      <div>
                        <strong>翻译剪贴板并插入</strong>
                        <p>不核验原目标，可能粘贴到其他窗口。</p>
                      </div>
                      <ClipboardGesture platform={platform.os} count={3} />
                    </div>
                    <div className="hotkey-row">
                      <strong>翻译剪贴板并打开 WebUI</strong>
                      <ClipboardGesture platform={platform.os} count={4} />
                    </div>
                  </section>
                  <section aria-label="通用操作">
                    <h3>通用操作</h3>
                    <HotkeyRecorder
                      label="打开 WebUI（不翻译）"
                      value={draft.hotkeys.openUi}
                      disabled={!platform.hotkeysAvailable || action.busy}
                      isMac={platform.os === "darwin"}
                      onChange={(value) =>
                        setDraft({
                          ...draft,
                          hotkeys: { ...draft.hotkeys, openUi: value },
                        })
                      }
                    />
                  </section>
                </div>
                <details className="shortcut-diagnostics">
                  <summary>
                    <ChevronRight size={16} aria-hidden="true" />
                    最近快捷键状态
                    {shortcut?.action
                      ? ` · ${getShortcutStageLabel(shortcut.stage)}`
                      : ""}
                  </summary>
                  <Notice>{platformError}</Notice>
                  <Notice>
                    {!shortcut ? (
                      "当前会话无快捷键诊断。"
                    ) : !shortcut.action ? (
                      "尚未收到快捷键"
                    ) : (
                      <>
                        <strong>
                          {getShortcutActionLabel(shortcut.action)}
                        </strong>
                        <p>
                          最近事件时间：
                          <time dateTime={shortcut.occurredAt}>
                            {Number.isNaN(Date.parse(shortcut.occurredAt))
                              ? "未知"
                              : new Date(
                                  shortcut.occurredAt,
                                ).toLocaleString()}
                          </time>
                        </p>
                        <p>{getShortcutStageLabel(shortcut.stage)}</p>
                        <p>
                          诊断阶段：<code>{shortcut.stage}</code>
                          {shortcut.message && <> · {shortcut.message}</>}
                        </p>
                        {shortcut.taskId && (
                          <a
                            href={`?task=${encodeURIComponent(shortcut.taskId)}`}
                          >
                            查看关联翻译任务
                          </a>
                        )}
                      </>
                    )}
                  </Notice>
                </details>
              </section>
            </Tabs.Content>
            <Tabs.Content value="service">
              <section className="card settings-card">
                <div className="section-title">
                  <h2>本地服务与平台</h2>
                  <button
                    type="button"
                    className="icon-button tooltip-trigger"
                    aria-label="刷新状态"
                    onClick={() => {
                      void platformWatcher.current?.refresh();
                    }}
                  >
                    <RefreshCw size={16} aria-hidden="true" />
                    <span className="control-tooltip" aria-hidden="true">
                      刷新状态
                    </span>
                  </button>
                </div>
                <div className="settings-row">
                  <label htmlFor="settings-port">监听端口</label>
                  <div className="settings-control">
                    <input
                      id="settings-port"
                      type="number"
                      aria-describedby="settings-port-help"
                      required
                      min={1024}
                      max={65535}
                      value={draft.port}
                      onChange={(event) =>
                        setDraft({
                          ...draft,
                          port: Number(event.target.value),
                        })
                      }
                    />
                    <small id="settings-port-help">
                      下次重启生效。当前页面端口：{location.port || "默认"}
                    </small>
                  </div>
                </div>
                <dl className="platform-info">
                  <div className="settings-row">
                    <dt>操作系统</dt>
                    <dd>
                      {platform.os === "windows"
                        ? "Windows"
                        : platform.os === "darwin"
                          ? "macOS"
                          : platform.os}
                    </dd>
                  </div>
                  <div className="settings-row">
                    <dt>全局快捷键</dt>
                    <dd>
                      <AvailabilityBadge available={platform.hotkeysAvailable}>
                        {platform.hotkeysAvailable ? "可用" : "不可用"}
                      </AvailabilityBadge>
                    </dd>
                  </div>
                  <div className="settings-row">
                    <dt>安全粘贴</dt>
                    <dd>
                      <AvailabilityBadge available={platform.pasteAvailable}>
                        {platform.pasteAvailable
                          ? "可用（按目标检查）"
                          : "不可用 / 手动复制"}
                      </AvailabilityBadge>
                    </dd>
                  </div>
                </dl>
                <Notice error>{platformError}</Notice>
              </section>
            </Tabs.Content>
            <Tabs.Content value="about">
              <VersionSection state={state} />
            </Tabs.Content>
            {(dirty || action.busy) && (
              <div className="settings-save" ref={saveBar}>
                <span className="settings-save-status" role="status">
                  <CircleAlert size={16} aria-hidden="true" />
                  {action.busy ? "正在保存设置…" : "有尚未保存的修改"}
                </span>
                <div className="button-row">
                  <button
                    type="button"
                    onClick={() => {
                      restoreTabFocus();
                      setDraft(copySettings(state.settings));
                      action.setError("");
                    }}
                  >
                    放弃修改
                  </button>
                  <button className="primary" type="submit" aria-busy={action.busy}>
                    <Save size={16} />
                    {action.busy ? "正在保存…" : "保存设置"}
                  </button>
                </div>
              </div>
            )}
          </fieldset>
        </form>
      </Tabs.Root>
    </div>
  );
}
