import { request } from "./api";
import type { AppState } from "./types";

const actionLabels: Record<string, string> = {
  "translate-paste": "翻译剪贴板并粘贴",
  "translate-ui": "翻译剪贴板并打开 WebUI",
  "open-ui": "打开 WebUI（不翻译）",
};

const stageLabels: Record<string, string> = {
  received: "已收到快捷键，正在准备处理。",
  capturing: "快捷键已触发，正在读取文本和原目标。",
  capture_failed: "快捷键已触发，但读取剪贴板失败，不是快捷键未触发。未发起翻译；请重新复制文本后再试。",
  starting: "已读取文本，正在创建翻译任务。",
  start_failed: "快捷键已触发，但无法创建翻译任务。请检查默认服务、提示词及语言设置后重试。",
  translating: "正在翻译。若需自动粘贴，请保持原目标和剪贴板不变，并松开快捷键。",
  opened: "已请求打开 WebUI；如未显示，请手动打开界面或查看关联任务。",
  open_failed: "打开 WebUI 失败；若已有任务，请通过下方链接查看，不必重复发起翻译。",
  translation_failed: "翻译失败。请查看任务错误并检查服务配置；不会自动重试。",
  pasting: "正在核验原目标和剪贴板，只有安全检查通过后才会发送粘贴。",
  paste_skipped: "已跳过自动粘贴，以保护原目标和剪贴板。请查看任务并手动复制译文；下次请保持原目标和剪贴板不变，并松开快捷键。",
  paste_dispatched: "已向经过核验的原目标发送粘贴，不代表应用已插入文字。请检查原目标；未插入时可从任务手动复制译文。",
  direct_translating: "试验模式正在翻译：完成后直接向当时的前台窗口发送粘贴，不检查原光标、选区或应用。请松开快捷键，避免复制其他内容。",
  direct_pasting: "试验模式正在准备粘贴：仅检查剪贴板和按键状态，不核验粘贴目标。",
  direct_paste_skipped: "试验粘贴未完成。此模式不检查原目标；请查看下方剪贴板、按键、任务或发送失败的具体原因，必要时从任务手动复制译文。",
  direct_paste_dispatched: "已向当时的前台窗口发送粘贴，未核验原目标，不代表应用已插入文字。切换窗口后可能粘贴到新窗口，请自行检查结果。",
  outcome_failed: "后续处理失败，不代表翻译一定失败。请查看任务及诊断信息，必要时手动复制译文。",
  cancelled: "本次快捷键操作已取消；如仍需翻译，请确认目标后重新触发。",
  busy: "已有快捷键操作正在处理，本次未启动新操作。请等待当前操作结束。",
};

export function getShortcutActionLabel(action: string): string {
  return Object.hasOwn(actionLabels, action) ? actionLabels[action] : "未知快捷键动作";
}

export function getShortcutStageLabel(stage: string): string {
  return Object.hasOwn(stageLabels, stage) ? stageLabels[stage] : "未知处理阶段，请查看诊断信息。";
}

// One request lane serves polling, focus and manual refresh. No form/app state is returned.
export function watchPlatformStatus(
  onPlatform: (platform: AppState["platform"]) => void,
  onError: (message: string) => void,
) {
  let stopped = false;
  let timer: ReturnType<typeof setTimeout> | undefined;
  let controller: AbortController | undefined;
  let pending: Promise<void> | undefined;
  let lastStartedAt = -Infinity;

  function clearTimer() {
    if (timer !== undefined) clearTimeout(timer);
    timer = undefined;
  }

  function refresh(): Promise<void> {
    if (stopped || document.visibilityState !== "visible") return Promise.resolve();
    if (pending) return pending;
    clearTimer();
    lastStartedAt = Date.now();
    const current = new AbortController();
    controller = current;
    pending = request<AppState>("/state", { signal: current.signal })
      .then((state) => {
        if (stopped || current.signal.aborted) return;
        onPlatform(state.platform);
        onError("");
      })
      .catch(() => {
        if (!stopped && !current.signal.aborted)
          onError("状态刷新失败，以下可能是旧状态；不会影响未保存的设置。可稍后点击“刷新状态”重试。");
      })
      .finally(() => {
        pending = undefined;
        controller = undefined;
        if (!stopped && document.visibilityState === "visible")
          timer = setTimeout(() => void refresh(), 1500);
      });
    return pending;
  }

  function resume() {
    clearTimer();
    if (document.visibilityState !== "visible") {
      controller?.abort();
      return;
    }
    if (pending) return;
    // Visibility and focus commonly arrive together: coalesce that event pair.
    const delay = Math.max(0, 250 - (Date.now() - lastStartedAt));
    timer = setTimeout(() => void refresh(), delay);
  }

  document.addEventListener("visibilitychange", resume);
  window.addEventListener("focus", resume);
  void refresh();
  return {
    refresh,
    stop() {
      stopped = true;
      clearTimer();
      controller?.abort();
      document.removeEventListener("visibilitychange", resume);
      window.removeEventListener("focus", resume);
    },
  };
}
