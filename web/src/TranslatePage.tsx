import { useEffect, useRef, useState } from "react";
import {
  ArrowLeftRight,
  Eraser,
  FileText,
  RotateCcw,
  Sparkles,
  Square,
} from "lucide-react";
import { request } from "./api";
import {
  AutoTranslationController,
  initialTranslationState,
} from "./auto-translation";
import { useSpeechSynthesis } from "./speech";
import {
  isActive,
  type AppState,
  type Task,
  type TranslationInput,
} from "./types";
import { LanguageOptions } from "./LanguageOptions";
import { CompactSelect } from "./CompactSelect";
import {
  MarkdownModeSwitch,
  MarkdownPreview,
  SourceMarkdownEditor,
  type MarkdownMode,
  type SourceMarkdownEditorHandle,
} from "./MarkdownEditor";
import { CopyButton, Empty, Notice, SpeakButton, Status } from "./ui";

export function TranslatePage({
  state,
  taskId,
  openTask,
  configure,
  saveDefaultProvider,
  savingDefaultProvider,
}: {
  state: AppState;
  taskId: string;
  openTask: (taskId: string) => void;
  configure: () => void;
  saveDefaultProvider: (providerId: string) => void;
  savingDefaultProvider: boolean;
}) {
  const providers = state.providers.filter((provider) => provider.enabled);
  const initialProvider =
    providers.find(
      (provider) => provider.id === state.settings.defaultProviderId,
    ) ?? providers[0];
  const initialProviderSupportsPrompts =
    state.providerTypes.find(
      (capabilities) => capabilities.type === initialProvider?.type,
    )?.supportsPrompts ?? false;
  const initialPrompt =
    state.prompts.find(
      (prompt) => prompt.id === state.settings.defaultPromptId,
    ) ?? state.prompts[0];
  const [initialInput] = useState<TranslationInput>(() => ({
    text: "",
    sourceLanguage: state.settings.sourceLanguage,
    targetLanguage: state.settings.targetLanguage,
    providerId: initialProvider?.id ?? "",
    promptId: initialProviderSupportsPrompts ? (initialPrompt?.id ?? "") : "",
    entry: "web",
  }));
  const [workspace, setWorkspace] = useState(() =>
    initialTranslationState(initialInput),
  );
  const [markdownMode, setMarkdownMode] =
    useState<MarkdownMode>("markdown");
  const sourceEditor = useRef<SourceMarkdownEditorHandle>(null);
  const controller = useRef<AutoTranslationController | null>(null);
  const {
    input,
    task,
    loading,
    submitting,
    waiting,
    cancelling,
    cancelled,
    error,
    pollError,
    cancellationWarning,
  } = workspace;
  const { text, sourceLanguage, targetLanguage, providerId, promptId } = input;
  useEffect(() => {
    const current = new AutoTranslationController(
      initialInput,
      {
        create: (body, signal) =>
          request<Task>("/tasks", { method: "POST", body, signal }),
        load: (identifier, signal) =>
          request<Task>(`/tasks/${encodeURIComponent(identifier)}`, { signal }),
        cancel: (identifier, signal) =>
          request<Task>(`/tasks/${encodeURIComponent(identifier)}/cancel`, {
            method: "POST",
            body: {},
            signal,
          }),
      },
      setWorkspace,
      openTask,
    );
    controller.current = current;
    return () => {
      current.dispose();
      controller.current = null;
    };
  }, [initialInput, openTask]);
  useEffect(() => {
    controller.current?.open(taskId);
  }, [taskId]);
  const {
    status: speechStatus,
    activeTarget: activeSpeechTarget,
    error: speechError,
    speak,
  } = useSpeechSynthesis(state.settings.speech);
  const selectedProvider = providers.find(
    (provider) => provider.id === providerId,
  );
  const supportsPrompts =
    state.providerTypes.find(
      (capabilities) => capabilities.type === selectedProvider?.type,
    )?.supportsPrompts ?? false;
  const configured =
    !!selectedProvider &&
    (!supportsPrompts ||
      state.prompts.some((prompt) => prompt.id === promptId));
  const count = Array.from(text).length;
  const active = isActive(task);
  const translation = task?.translation || "";
  const longestTextCharacterCount = Math.max(count, Array.from(translation).length);
  const textDensity = longestTextCharacterCount > 10000
    ? "dense"
    : longestTextCharacterCount > 3000 ? "compact" : "regular";
  function changeInput(changes: Partial<TranslationInput>) {
    const updated = { ...input, ...changes };
    const provider = providers.find(
      (candidate) => candidate.id === updated.providerId,
    );
    const promptsRequired = state.providerTypes.find(
      (candidate) => candidate.type === provider?.type,
    )?.supportsPrompts;
    if (!promptsRequired) {
      updated.promptId = "";
    } else if (
      !state.prompts.some((prompt) => prompt.id === updated.promptId)
    ) {
      updated.promptId =
        state.prompts.find(
          (prompt) => prompt.id === state.settings.defaultPromptId,
        )?.id ?? state.prompts[0]?.id ?? "";
    }
    const available =
      !!provider &&
      (!promptsRequired ||
        state.prompts.some((prompt) => prompt.id === updated.promptId));
    controller.current?.update(updated, available);
  }
  function retryTranslation() {
    changeInput({});
    controller.current?.retry();
  }
  return (
    <div className="translate-page">
      {!configured && (
        <Notice>
          {!selectedProvider ? "未选择可用服务" : "未选择提示词模板"}
          <button className="text-button" onClick={configure}>
            配置服务 →
          </button>
        </Notice>
      )}
      <Notice error>
        {error && (
          <>
            {error}{" "}
            {!task && !submitting && !waiting && (
              <button
                disabled={!configured || !text.trim() || count > 20000}
                onClick={retryTranslation}
              >
                重试
              </button>
            )}
          </>
        )}
      </Notice>
      {cancellationWarning && (
        <Notice>
          {cancellationWarning.message}{" "}
          <a
            href={
              cancellationWarning.taskId
                ? `?task=${encodeURIComponent(cancellationWarning.taskId)}`
                : "?page=history"
            }
          >
            查看记录
          </a>
        </Notice>
      )}
      <Notice error>{speechError}</Notice>
      <Notice error>
        {pollError && (
          <>
            {pollError}{" "}
            <button
              disabled={cancelling}
              onClick={() => controller.current?.refresh()}
            >
              重新获取任务
            </button>
          </>
        )}
      </Notice>
      <section className="card translation-workspace" aria-label="翻译编辑器">
        <div className="language-bar" data-translation-control>
          <label className="language-select">
            <span className="sr-only">源语言</span>
            <select
              value={sourceLanguage}
              disabled={loading}
              onChange={(event) =>
                changeInput({
                  sourceLanguage: event.target.value,
                  targetLanguage:
                    event.target.value === "auto" ? "auto" : targetLanguage,
                })
              }
            >
              <LanguageOptions state={state} source />
            </select>
          </label>
          <button
            className="icon-button swap"
            title={
              sourceLanguage === "auto" || targetLanguage === "auto"
                ? "自动语言选择不能交换"
                : "交换源语言和目标语言"
            }
            aria-label="交换语言"
            disabled={
              sourceLanguage === "auto" || targetLanguage === "auto" || loading
            }
            onClick={() => {
              changeInput({
                sourceLanguage: targetLanguage,
                targetLanguage: sourceLanguage,
              });
            }}
          >
            <ArrowLeftRight size={18} />
          </button>
          <label className="language-select">
            <span className="sr-only">目标语言</span>
            <select
              value={targetLanguage}
              disabled={loading}
              onChange={(event) =>
                changeInput({ targetLanguage: event.target.value })
              }
            >
              <LanguageOptions state={state} automaticTarget />
            </select>
          </label>
        </div>
        <div className="editor-columns" data-text-density={textDensity}>
          <div className="editor-pane">
            <SourceMarkdownEditor
              ref={sourceEditor}
              value={text}
              mode={markdownMode}
              onModeChange={setMarkdownMode}
              disabled={loading}
              invalid={count > 20000}
              onChange={(updatedText) => changeInput({ text: updatedText })}
              onBlur={(relatedTarget) => {
                if (
                  relatedTarget instanceof Element &&
                  relatedTarget.closest("[data-translation-control]")
                )
                  return;
                controller.current?.flush();
              }}
              onCompositionChange={(composing) => {
                controller.current?.setComposing(composing);
              }}
              onSubmit={() => controller.current?.flush()}
            />
            <div className={`pane-footer ${count > 20000 ? "invalid" : ""}`}>
              <span>{count.toLocaleString()} / 20,000 字符</span>
              <span className="pane-controls">
                <SpeakButton
                  text={text}
                  speaking={activeSpeechTarget === "source"}
                  loading={
                    activeSpeechTarget === "source" && speechStatus === "loading"
                  }
                  onToggle={() => speak("source", text, sourceLanguage)}
                />
                <button
                  className="icon-button"
                  aria-label="清空原文和结果"
                  title="清空"
                  data-translation-control
                  disabled={loading || !text}
                  onClick={() => changeInput({ text: "" })}
                >
                  <Eraser size={16} />
                </button>
              </span>
            </div>
          </div>
          <div className="editor-pane result-pane">
            <div className="pane-heading">
              <span>译文</span>
              <MarkdownModeSwitch
                label="译文"
                mode={markdownMode}
                disabled={loading}
                onModeChange={(updatedMode) => sourceEditor.current?.changeMode(updatedMode)}
              />
            </div>
            <div
              className="translation-output"
              aria-live="polite"
              aria-busy={active || loading || submitting || waiting}
            >
              {loading ? (
                <Empty title="正在获取任务…" />
              ) : waiting || submitting ? (
                <Empty title={waiting ? "等待输入完成…" : "正在提交…"} />
              ) : active ? (
                <Empty
                  title={
                    task?.status === "queued" ? "任务已进入队列" : "正在翻译"
                  }
                />
              ) : task?.translation ? (
                markdownMode === "markdown" ? (
                  <MarkdownPreview text={task.translation} />
                ) : (
                  <p className="preserve-text">{task.translation}</p>
                )
              ) : (
                <Empty
                  title={
                    cancelled || task?.status === "cancelled"
                      ? "翻译已取消"
                      : task?.status === "interrupted"
                        ? "翻译已中断"
                        : task?.status === "failed"
                          ? "翻译未完成"
                          : "暂无译文"
                  }
                >
                  {task?.errorMessage}
                </Empty>
              )}
              {(cancelled ||
                (task &&
                  ["failed", "cancelled", "interrupted"].includes(
                    task.status,
                  ))) && (
                <button disabled={!configured} onClick={retryTranslation}>
                  <RotateCcw size={15} />
                  重试
                </button>
              )}
            </div>
            <div className="pane-footer">
              <span className="pane-controls">
                {task && <Status task={task} />}
                {task?.status === "succeeded" && (
                  <span>
                    {`${(task.durationMs / 1000).toFixed(1)} 秒 · ${task.providerSnapshot.name}`}
                  </span>
                )}
              </span>
              <span className="pane-controls">
                <SpeakButton
                  text={translation}
                  speaking={activeSpeechTarget === "translation"}
                  loading={
                    activeSpeechTarget === "translation" &&
                    speechStatus === "loading"
                  }
                  onToggle={() =>
                    speak(
                      "translation",
                      translation,
                      task?.targetLanguage || targetLanguage,
                    )
                  }
                />
                <CopyButton key={task?.id || "empty"} text={translation} />
              </span>
            </div>
          </div>
        </div>
        <div className="translation-actions">
          <div className="translation-controls" data-translation-control>
            <span className="translation-status">
              自动翻译
              {selectedProvider?.type === "openai-compatible" && " · 可能计费"}
            </span>
            <CompactSelect
              label="翻译服务"
              icon={<Sparkles size={16} aria-hidden="true" />}
              value={providerId}
              placeholder="未选择服务"
              disabled={loading || savingDefaultProvider}
              options={providers.map((provider) => ({
                value: provider.id,
                label: provider.name,
                description: provider.model,
              }))}
              onValueChange={(updatedProviderId) => {
                changeInput({
                  providerId: updatedProviderId,
                  promptId: promptId || state.settings.defaultPromptId,
                });
                saveDefaultProvider(updatedProviderId);
              }}
            />
            {supportsPrompts && (
              <CompactSelect
                label="提示词模板"
                icon={<FileText size={16} aria-hidden="true" />}
                value={promptId}
                placeholder="未选择模板"
                disabled={loading}
                options={state.prompts.map((prompt) => ({
                  value: prompt.id,
                  label: prompt.name,
                }))}
                onValueChange={(updatedPromptId) =>
                  changeInput({ promptId: updatedPromptId })
                }
              />
            )}
          </div>
          <div className="button-row">
            {(active || submitting || waiting) && (
              <button
                data-translation-control
                disabled={cancelling}
                onClick={() => {
                  void controller.current?.cancel();
                }}
              >
                <Square size={14} />
                {cancelling ? "正在取消…" : "取消任务"}
              </button>
            )}
          </div>
        </div>
      </section>
      <p className="footnote">
        {state.settings.speech.engine === "edge"
          ? "朗读发送至 Microsoft"
          : "浏览器朗读可能使用在线声音"}{" "}
        · 音频不保存
      </p>
    </div>
  );
}
