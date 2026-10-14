// @vitest-environment jsdom
import { useState } from "react";
import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
  within,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { request, requestAudio } from "./api";
import { App } from "./App";
import { FeedbackProvider, useFeedback } from "./feedback";
import { SettingsPage } from "./SettingsPage";
import { TranslatePage } from "./TranslatePage";
import { ThemeToggle } from "./ThemeToggle";
import {
  createSpeechEvent,
  createTestAppState,
  createTestSynthesis,
  createTestVoice,
  TestUtterance,
} from "./speech-test-fixtures";
import type { SpeechAudio } from "./speech";
import type { Settings, SpeechSettings, Task } from "./types";

vi.mock("./api", async (importOriginal) => ({
  ...(await importOriginal<typeof import("./api")>()),
  request: vi.fn(),
  requestAudio: vi.fn(),
}));
vi.mock("./shortcut-status", async (importOriginal) => ({
  ...(await importOriginal<typeof import("./shortcut-status")>()),
  watchPlatformStatus: () => ({ stop: vi.fn(), refresh: vi.fn() }),
}));

beforeEach(() => {
  vi.useFakeTimers();
  vi.mocked(request).mockReset();
  vi.mocked(requestAudio).mockReset();
  window.history.replaceState(null, "", "/");
});
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
  vi.clearAllTimers();
  vi.useRealTimers();
  window.history.replaceState(null, "", "/");
});

function selectTab(name: string) {
  fireEvent.mouseDown(screen.getByRole("tab", { name }), {
    button: 0,
    ctrlKey: false,
  });
}

describe("topbar theme control", () => {
  afterEach(() => {
    window.localStorage.removeItem("cvtranslate.theme");
    document.documentElement.removeAttribute("data-theme");
    document.documentElement.classList.remove("dark-theme");
  });

  it("sits beside GitHub and preserves unsaved settings without backend writes", async () => {
    window.history.replaceState(null, "", "/?page=settings");
    vi.mocked(request).mockResolvedValueOnce(createTestAppState());
    await act(async () => {
      render(<FeedbackProvider><App /></FeedbackProvider>);
    });
    const targetLanguage = screen.getByLabelText("默认目标语言") as HTMLSelectElement;
    fireEvent.change(targetLanguage, { target: { value: "en" } });
    const toggle = screen.getByRole("button", { name: "切换到暗色模式" });
    const githubLink = within(toggle.parentElement!).getByRole("link", { name: "GitHub 仓库" });
    expect(toggle.nextElementSibling).toBe(githubLink);
    expect(toggle.closest(".topbar")).not.toBeNull();
    fireEvent.click(toggle);
    expect(document.documentElement.dataset.theme).toBe("dark");
    expect(screen.getByLabelText("默认目标语言")).toBe(targetLanguage);
    expect(targetLanguage.value).toBe("en");
    expect(screen.getByRole("button", { name: "保存设置" })).toBeTruthy();
    expect(request).toHaveBeenCalledExactlyOnceWith("/state", { signal: expect.any(AbortSignal) });
    expect(requestAudio).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("link", { name: "服务提供商" }));
    expect(document.documentElement.dataset.theme).toBe("dark");
    expect(screen.getByRole("button", { name: "切换到亮色模式" })).toBe(toggle);
  });

  it("does not flush pending translation or reset source text when the editor loses focus to it", async () => {
    render(
      <FeedbackProvider>
        <ThemeToggle />
        <TranslatePage
          state={createWorkspaceState()}
          taskId=""
          openTask={vi.fn()}
          configure={vi.fn()}
          saveDefaultProvider={vi.fn()}
          savingDefaultProvider={false}
        />
      </FeedbackProvider>,
    );
    fireEvent.click(screen.getByRole("button", { name: "原文 Markdown 源码" }));
    const source = screen.getByRole("textbox", { name: "待翻译原文" }) as HTMLTextAreaElement;
    fireEvent.change(source, { target: { value: "Draft text" } });
    const toggle = screen.getByRole("button", { name: "切换到暗色模式" });
    await act(async () => {
      fireEvent.blur(source, { relatedTarget: toggle });
      fireEvent.click(toggle);
      await vi.advanceTimersByTimeAsync(100);
    });
    expect(document.documentElement.dataset.theme).toBe("dark");
    expect(screen.getByRole("textbox", { name: "待翻译原文" })).toBe(source);
    expect(source.value).toBe("Draft text");
    expect(request).not.toHaveBeenCalled();
    expect(requestAudio).not.toHaveBeenCalled();
  });
});

describe("settings tabs", () => {
  it("only shows save actions for changes and removes them when an edit is reverted", () => {
    const state = createTestAppState();
    const { container } = render(
      <FeedbackProvider>
        <SettingsPage state={state} updateState={vi.fn()} />
      </FeedbackProvider>,
    );
    expect(container.querySelector(".settings-save")).toBeNull();
    expect(screen.queryByRole("button", { name: "保存设置" })).toBeNull();
    expect(screen.queryByRole("button", { name: "放弃修改" })).toBeNull();
    fireEvent.change(screen.getByLabelText("默认目标语言"), { target: { value: "en" } });
    expect(screen.getByRole("button", { name: "保存设置" })).toBeTruthy();
    fireEvent.change(screen.getByLabelText("默认目标语言"), {
      target: { value: state.settings.targetLanguage },
    });
    expect(container.querySelector(".settings-save")).toBeNull();
    expect(request).not.toHaveBeenCalled();
  });

  it("discards changes across tabs and returns focus to the active tab", () => {
    const state = createTestAppState();
    render(
      <FeedbackProvider>
        <SettingsPage state={state} updateState={vi.fn()} />
      </FeedbackProvider>,
    );
    fireEvent.change(screen.getByLabelText("默认目标语言"), { target: { value: "en" } });
    selectTab("朗读");
    fireEvent.change(screen.getByLabelText("朗读引擎"), { target: { value: "web-speech" } });
    selectTab("本地服务");
    fireEvent.change(screen.getByRole("spinbutton"), { target: { value: "19330" } });
    const discard = screen.getByRole("button", { name: "放弃修改" });
    discard.focus();
    fireEvent.click(discard);
    expect(document.activeElement).toBe(screen.getByRole("tab", { name: "本地服务" }));
    expect(screen.queryByRole("button", { name: "保存设置" })).toBeNull();
    expect(screen.queryByRole("button", { name: "放弃修改" })).toBeNull();
    expect((screen.getByRole("spinbutton") as HTMLInputElement).value).toBe(String(state.settings.port));
    selectTab("朗读");
    expect((screen.getByLabelText("朗读引擎") as HTMLSelectElement).value).toBe(state.settings.speech.engine);
    selectTab("翻译");
    expect((screen.getByLabelText("默认目标语言") as HTMLSelectElement).value).toBe(state.settings.targetLanguage);
    expect(request).not.toHaveBeenCalled();
    expect(requestAudio).not.toHaveBeenCalled();
  });

  it("removes saved actions and restores active-tab focus after the host accepts saved settings", async () => {
    const initialState = createTestAppState();
    const savedSettings = { ...initialState.settings, targetLanguage: "en" };
    function SettingsHost() {
      const [state, setState] = useState(initialState);
      return <SettingsPage state={state} updateState={setState} />;
    }
    vi.mocked(request).mockResolvedValueOnce(savedSettings);
    const { container } = render(<FeedbackProvider><SettingsHost /></FeedbackProvider>);
    fireEvent.change(screen.getByLabelText("默认目标语言"), { target: { value: "en" } });
    selectTab("朗读");
    const saveButton = screen.getByRole("button", { name: "保存设置" });
    saveButton.focus();
    await act(async () => { fireEvent.click(saveButton); });
    expect(request).toHaveBeenCalledExactlyOnceWith("/settings", {
      method: "PUT", body: savedSettings, signal: expect.any(AbortSignal),
    });
    expect(container.querySelector(".settings-save")).toBeNull();
    expect(document.activeElement).toBe(screen.getByRole("tab", { name: "朗读" }));
    expect(screen.getByText("设置已保存")).toBeTruthy();
    selectTab("翻译");
    expect((screen.getByLabelText("默认目标语言") as HTMLSelectElement).value).toBe("en");
    fireEvent.change(screen.getByLabelText("默认目标语言"), { target: { value: "zh-CN" } });
    fireEvent.click(screen.getByRole("button", { name: "放弃修改" }));
    expect((screen.getByLabelText("默认目标语言") as HTMLSelectElement).value).toBe("en");
  });

  it("retains a failed save draft without retrying or autosaving on tab changes", async () => {
    const state = createTestAppState();
    const originalSettings = structuredClone(state.settings);
    const updateState = vi.fn();
    vi.mocked(request).mockRejectedValueOnce(new Error("保存失败"));
    render(<FeedbackProvider><SettingsPage state={state} updateState={updateState} /></FeedbackProvider>);
    fireEvent.change(screen.getByLabelText("默认目标语言"), { target: { value: "en" } });
    await act(async () => { fireEvent.click(screen.getByRole("button", { name: "保存设置" })); });
    expect(screen.getByRole("alert").textContent).toContain("保存失败");
    expect((screen.getByRole("button", { name: "保存设置" }) as HTMLButtonElement).disabled).toBe(false);
    expect(screen.getByRole("button", { name: "放弃修改" })).toBeTruthy();
    selectTab("朗读");
    selectTab("翻译");
    await act(async () => { await vi.advanceTimersByTimeAsync(60_000); });
    expect((screen.getByLabelText("默认目标语言") as HTMLSelectElement).value).toBe("en");
    expect(screen.getByText("有尚未保存的修改")).toBeTruthy();
    expect(request).toHaveBeenCalledOnce();
    expect(requestAudio).not.toHaveBeenCalled();
    expect(updateState).not.toHaveBeenCalled();
    expect(state.settings).toEqual(originalSettings);
  });

  it("describes disabled prompts only when the selected provider lacks prompt support", () => {
    const state = createTestAppState();
    state.providers = [
      { id: "microsoft", name: "Microsoft", type: "microsoft-translate", baseUrl: "", model: "", timeoutSeconds: 30, enabled: true, hasApiKey: false },
      { id: "openai", name: "OpenAI", type: "openai-compatible", baseUrl: "https://example.test", model: "test-model", timeoutSeconds: 30, enabled: true, hasApiKey: false },
    ];
    state.providerTypes = state.providers.map((provider) => ({
      type: provider.type, name: provider.name, defaultModel: provider.model,
      supportsPrompts: provider.id === "openai", supportsApiKey: provider.id === "openai",
      configurableEndpoint: provider.id === "openai", requiresModel: provider.id === "openai",
    }));
    render(<FeedbackProvider><SettingsPage state={state} updateState={vi.fn()} /></FeedbackProvider>);
    const prompt = screen.getByRole("combobox", { name: "默认提示词" }) as HTMLSelectElement;
    expect(prompt.disabled).toBe(true);
    expect(document.getElementById(prompt.getAttribute("aria-describedby")!)?.textContent).toBe("请先选择默认服务");
    fireEvent.change(screen.getByLabelText("默认服务"), { target: { value: "microsoft" } });
    expect(prompt.disabled).toBe(true);
    expect(prompt.required).toBe(false);
    expect(document.getElementById(prompt.getAttribute("aria-describedby")!)?.textContent).toBe("当前服务不使用提示词");
    fireEvent.change(screen.getByLabelText("默认服务"), { target: { value: "openai" } });
    expect(prompt.disabled).toBe(false);
    expect(prompt.required).toBe(true);
    expect(prompt.hasAttribute("aria-describedby")).toBe(false);
    expect(screen.queryByText("当前服务不使用提示词")).toBeNull();
    fireEvent.change(screen.getByLabelText("默认服务"), { target: { value: "microsoft" } });
    expect(prompt.disabled).toBe(true);
    expect(document.getElementById(prompt.getAttribute("aria-describedby")!)?.textContent).toBe("当前服务不使用提示词");
    expect(request).not.toHaveBeenCalled();
  });

  it("confirms history clearing without saving or discarding settings drafts", async () => {
    const state = createTestAppState();
    const originalSettings = structuredClone(state.settings);
    const updateState = vi.fn();
    let finishClear = () => {};
    vi.mocked(request).mockReturnValueOnce(new Promise<void>((resolve) => {
      finishClear = resolve;
    }));
    render(
      <FeedbackProvider>
        <SettingsPage state={state} updateState={updateState} />
      </FeedbackProvider>,
    );
    fireEvent.change(screen.getByLabelText("默认目标语言"), {
      target: { value: "en" },
    });
    const clearButton = screen.getByRole("button", {
      name: "清空历史",
    }) as HTMLButtonElement;
    fireEvent.click(clearButton);
    expect(screen.getByRole("alertdialog").textContent).toContain("永久删除，无法撤销。");
    expect(request).not.toHaveBeenCalled();
    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: "取消" }));
    });
    expect(request).not.toHaveBeenCalled();

    fireEvent.click(clearButton);
    await act(async () => {
      fireEvent.click(within(screen.getByRole("alertdialog")).getByRole("button", {
        name: "清空历史",
      }));
    });
    expect(request).toHaveBeenCalledExactlyOnceWith("/history", {
      method: "DELETE",
      signal: expect.any(AbortSignal),
    });
    expect(clearButton.disabled).toBe(true);
    fireEvent.click(clearButton);
    expect(screen.queryByRole("alertdialog")).toBeNull();
    await act(async () => finishClear());
    expect(clearButton.disabled).toBe(false);
    expect(screen.getByText("历史记录已清空。")).toBeTruthy();
    expect((screen.getByLabelText("默认目标语言") as HTMLSelectElement).value).toBe("en");
    expect(screen.getByText("有尚未保存的修改")).toBeTruthy();
    expect(state.settings).toEqual(originalSettings);
    expect(updateState).not.toHaveBeenCalled();
    expect(request).toHaveBeenCalledOnce();
  });

  it("shows history clearing failures without retrying or changing settings", async () => {
    const updateState = vi.fn();
    vi.mocked(request).mockRejectedValueOnce(new Error("仍有翻译任务正在运行。"));
    render(
      <FeedbackProvider>
        <SettingsPage state={createTestAppState()} updateState={updateState} />
      </FeedbackProvider>,
    );
    fireEvent.click(screen.getByRole("button", { name: "清空历史" }));
    await act(async () => {
      fireEvent.click(within(screen.getByRole("alertdialog")).getByRole("button", {
        name: "清空历史",
      }));
    });
    expect(screen.getByRole("alert").textContent).toContain("仍有翻译任务正在运行。");
    expect((screen.getByRole("button", { name: "清空历史" }) as HTMLButtonElement).disabled)
      .toBe(false);
    expect(screen.queryByText("历史记录已清空。")).toBeNull();
    expect(request).toHaveBeenCalledExactlyOnceWith("/history", {
      method: "DELETE",
      signal: expect.any(AbortSignal),
    });
    expect(updateState).not.toHaveBeenCalled();
  });

  it("shows fixed clipboard gestures and records Ctrl+Space for open-WebUI", () => {
    render(
      <FeedbackProvider>
        <SettingsPage state={createTestAppState()} updateState={vi.fn()} />
      </FeedbackProvider>,
    );
    selectTab("快捷键");
    expect(screen.getByText("Ctrl+C 连按 3 次")).toBeTruthy();
    expect(screen.getByText("Ctrl+C 连按 4 次")).toBeTruthy();
    expect(screen.queryByText("选中文本翻译")).toBeNull();

    const recorder = screen.getByRole("button", { name: /打开 WebUI（不翻译）/ });
    fireEvent.click(recorder);
    fireEvent.keyDown(recorder, { key: " ", ctrlKey: true });
    expect(screen.getByRole("button", { name: "打开 WebUI（不翻译）：Ctrl+Space，点击录制" })).toBe(recorder);
    expect(Array.from(recorder.querySelectorAll(".shortcut-keys kbd"), (keycap) => keycap.textContent)).toEqual(["Ctrl", "Space"]);
    expect(recorder.querySelector(".shortcut-keys")?.getAttribute("aria-hidden")).toBe("true");
  });

  it.each(["Escape", "Tab", "blur"])("leaves shortcut recording on %s without changing the saved keycaps", (exitAction) => {
    const state = createTestAppState();
    state.settings.hotkeys.openUi = "Ctrl+Space";
    render(<FeedbackProvider><SettingsPage state={state} updateState={vi.fn()} /></FeedbackProvider>);
    selectTab("快捷键");
    const recorder = screen.getByRole("button", { name: /打开 WebUI（不翻译）/ });
    fireEvent.click(recorder);
    expect(recorder.getAttribute("aria-pressed")).toBe("true");
    if (exitAction === "blur") fireEvent.blur(recorder);
    else fireEvent.keyDown(recorder, { key: exitAction });
    expect(recorder.getAttribute("aria-pressed")).toBe("false");
    expect(screen.getByRole("button", { name: "打开 WebUI（不翻译）：Ctrl+Space，点击录制" })).toBe(recorder);
    expect(Array.from(recorder.querySelectorAll("kbd"), (keycap) => keycap.textContent)).toEqual(["Ctrl", "Space"]);
    expect(screen.queryByRole("button", { name: "保存设置" })).toBeNull();
    expect(request).not.toHaveBeenCalled();
  });

  it("records modified Enter without submitting and Delete disables the shortcut", () => {
    render(<FeedbackProvider><SettingsPage state={createTestAppState()} updateState={vi.fn()} /></FeedbackProvider>);
    selectTab("快捷键");
    const recorder = screen.getByRole("button", { name: /打开 WebUI（不翻译）/ });
    fireEvent.click(recorder);
    expect(fireEvent.keyDown(recorder, { key: "Enter", ctrlKey: true })).toBe(false);
    expect(screen.getByRole("button", { name: "打开 WebUI（不翻译）：Ctrl+Return，点击录制" })).toBe(recorder);
    expect(Array.from(recorder.querySelectorAll("kbd"), (keycap) => keycap.textContent)).toEqual(["Ctrl", "Return"]);
    expect(screen.getByRole("button", { name: "保存设置" })).toBeTruthy();
    expect(request).not.toHaveBeenCalled();
    fireEvent.click(recorder);
    fireEvent.keyDown(recorder, { key: "Delete" });
    expect(screen.getByRole("button", { name: "打开 WebUI（不翻译）：已禁用，点击录制" })).toBe(recorder);
    expect(recorder.querySelector("kbd")).toBeNull();
    expect(recorder.getAttribute("aria-pressed")).toBe("false");
    expect(screen.queryByRole("button", { name: "保存设置" })).toBeNull();
    expect(request).not.toHaveBeenCalled();
  });

  it("shows fixed macOS gestures and prevents saving ordinary Command+C", () => {
    const state = createTestAppState();
    state.platform.os = "darwin";
    render(
      <FeedbackProvider>
        <SettingsPage state={state} updateState={vi.fn()} />
      </FeedbackProvider>,
    );
    selectTab("快捷键");
    expect(screen.getByText("Command+C 连按 3 次")).toBeTruthy();
    expect(screen.getByText("Command+C 连按 4 次")).toBeTruthy();
    expect(screen.getByText("不核验原目标，可能粘贴到其他窗口。")).toBeTruthy();
    expect(screen.getByText("需手动授予辅助功能和输入监控权限，并重启应用。")).toBeTruthy();
    expect(screen.queryByText("Ctrl+C 连按 3 次")).toBeNull();
    expect(screen.queryByText("选中文本翻译")).toBeNull();
    expect(screen.getAllByRole("button", { name: /点击录制/ })).toHaveLength(1);

    const recorder = screen.getByRole("button", { name: /打开 WebUI（不翻译）/ });
    fireEvent.click(recorder);
    fireEvent.keyDown(recorder, { key: "c", metaKey: true });
    selectTab("翻译");
    fireEvent.click(screen.getByRole("button", { name: "保存设置" }));
    expect(screen.getByRole("tab", { name: "快捷键" }).getAttribute("aria-selected"))
      .toBe("true");
    expect(screen.getByRole("alert").textContent).toContain("普通 Command+C");
    expect(request).not.toHaveBeenCalled();
  });

  it("retains the unsupported-platform display without advertising macOS gestures", () => {
    const state = createTestAppState();
    state.platform.os = "linux";
    state.platform.hotkeysAvailable = false;
    render(
      <FeedbackProvider>
        <SettingsPage state={state} updateState={vi.fn()} />
      </FeedbackProvider>,
    );
    selectTab("快捷键");
    expect(screen.getByText("连续复制触发仅支持 Windows 和 macOS。")).toBeTruthy();
    expect(screen.getAllByText("当前平台不可用")).toHaveLength(2);
    expect(screen.queryByText("Command+C 连按 3 次")).toBeNull();
    expect((screen.getByRole("button", { name: /打开 WebUI（不翻译）/ }) as HTMLButtonElement).disabled)
      .toBe(true);
  });

  it("retains drafts across unmounted panels and saves all tabs with one request", async () => {
    const state = createTestAppState();
    const originalSettings = structuredClone(state.settings);
    state.speechVoices.push({
      id: "english-alternative",
      language: "en",
      name: "Alternative",
    });
    const updateState = vi.fn();
    const { container } = render(
      <FeedbackProvider>
        <SettingsPage state={state} updateState={updateState} />
      </FeedbackProvider>,
    );
    expect(screen.getAllByRole("tab")).toHaveLength(5);
    expect(container.querySelectorAll("form")).toHaveLength(1);
    expect(screen.queryByRole("combobox", { name: "朗读引擎" })).toBeNull();
    fireEvent.change(screen.getByLabelText("默认目标语言"), {
      target: { value: "en" },
    });
    selectTab("朗读");
    fireEvent.change(screen.getByLabelText("英语声音"), {
      target: { value: "english-alternative" },
    });
    selectTab("本地服务");
    expect(screen.queryByRole("button", { name: "退出应用" })).toBeNull();
    fireEvent.change(screen.getByRole("spinbutton"), {
      target: { value: "19330" },
    });
    selectTab("翻译");
    expect(
      (screen.getByLabelText("默认目标语言") as HTMLSelectElement).value,
    ).toBe("en");
    selectTab("朗读");
    expect((screen.getByLabelText("英语声音") as HTMLSelectElement).value).toBe(
      "english-alternative",
    );
    expect(request).not.toHaveBeenCalled();
    expect(requestAudio).not.toHaveBeenCalled();
    expect(state.settings).toEqual(originalSettings);
    const expectedSettings = {
      ...originalSettings,
      targetLanguage: "en",
      port: 19330,
      speech: {
        ...originalSettings.speech,
        edgeVoices: {
          ...originalSettings.speech.edgeVoices,
          en: "english-alternative",
        },
      },
    };
    vi.mocked(request).mockResolvedValueOnce(expectedSettings);
    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: "保存设置" }));
    });
    expect(request).toHaveBeenCalledExactlyOnceWith("/settings", {
      method: "PUT",
      body: expectedSettings,
      signal: expect.any(AbortSignal),
    });
    expect(updateState).toHaveBeenCalledExactlyOnceWith({
      ...state,
      settings: expectedSettings,
    });
  });

  it("selects the invalid hidden panel instead of bypassing its validation", () => {
    render(
      <FeedbackProvider>
        <SettingsPage state={createTestAppState()} updateState={vi.fn()} />
      </FeedbackProvider>,
    );
    selectTab("本地服务");
    fireEvent.change(screen.getByRole("spinbutton"), {
      target: { value: "99" },
    });
    selectTab("翻译");
    fireEvent.click(screen.getByRole("button", { name: "保存设置" }));
    expect(
      screen
        .getByRole("tab", { name: "本地服务" })
        .getAttribute("aria-selected"),
    ).toBe("true");
    expect(screen.getByRole("alert").textContent).toContain("1024");
    expect(request).not.toHaveBeenCalled();
  });

  it("stops a pending voice preview when leaving its tab without saving", () => {
    vi.mocked(requestAudio).mockReturnValue(new Promise(() => {}));
    render(
      <FeedbackProvider>
        <SettingsPage state={createTestAppState()} updateState={vi.fn()} />
      </FeedbackProvider>,
    );
    selectTab("朗读");
    expect(requestAudio).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: "试听：英语声音" }));
    expect(requestAudio).toHaveBeenCalledOnce();
    const signal = vi.mocked(requestAudio).mock.calls[0][1].signal;
    expect(signal?.aborted).toBe(false);
    selectTab("翻译");
    expect(signal?.aborted).toBe(true);
    expect(request).not.toHaveBeenCalled();
  });
});

function createCompletedTask(translation: string): Task {
  return {
    id: "completed",
    text: "# Source heading",
    translation,
    sourceLanguage: "en",
    targetLanguage: "zh-CN",
    status: "succeeded",
    entry: "web",
    providerSnapshot: {
      id: "provider",
      name: "Test service",
      type: "microsoft-translate",
      baseUrl: "",
      model: "",
      timeoutSeconds: 30,
      enabled: true,
      hasApiKey: false,
    },
    promptSnapshot: {
      id: "",
      name: "",
      persona: "",
      revision: 0,
      builtIn: false,
      systemTemplate: "",
      userTemplate: "",
    },
    errorCode: "",
    errorMessage: "",
    durationMs: 20,
    createdAt: "",
    updatedAt: "",
    pasteStatus: "",
  };
}

function createWorkspaceState() {
  const state = createTestAppState();
  state.providers = [
    {
      id: "provider",
      name: "Test service",
      type: "microsoft-translate",
      baseUrl: "",
      model: "",
      timeoutSeconds: 30,
      enabled: true,
      hasApiKey: false,
    },
  ];
  state.providerTypes = [
    {
      type: "microsoft-translate",
      name: "Test",
      defaultModel: "",
      supportsPrompts: false,
      supportsApiKey: false,
      configurableEndpoint: false,
      requiresModel: false,
    },
  ];
  state.settings.defaultProviderId = "provider";
  state.settings.targetLanguage = "auto";
  return state;
}

function renderWorkspace(taskId = "", state = createWorkspaceState()) {
  const openTask = vi.fn();
  const props = {
    state,
    taskId,
    openTask,
    configure: vi.fn(),
    saveDefaultProvider: vi.fn(),
    savingDefaultProvider: false,
  };
  const view = render(
    <FeedbackProvider>
      <TranslatePage {...props} />
    </FeedbackProvider>,
  );
  if (!taskId) {
    fireEvent.click(screen.getByRole("button", { name: "原文 Markdown 源码" }));
  }
  return {
    ...view,
    props,
    openTask,
    source: screen.getByRole("textbox", {
      name: "待翻译原文",
    }) as HTMLTextAreaElement,
  };
}

function mockWorkspaceSpeech(engine: SpeechSettings["engine"]) {
  const audio: SpeechAudio = {
    src: "",
    onended: null,
    onerror: null,
    play: vi.fn().mockResolvedValue(undefined),
    pause: vi.fn(),
    load: vi.fn(),
    removeAttribute: vi.fn(),
  };
  const synthesis = createTestSynthesis([
    createTestVoice("english", "en-US"),
    createTestVoice("chinese", "zh-CN"),
  ]);
  const revokeUrl = vi.fn();
  vi.stubGlobal("Audio", vi.fn(function () { return audio; }));
  vi.stubGlobal("URL", class extends URL {
    static createObjectURL = vi.fn(() => "blob:workspace-speech");
    static revokeObjectURL = revokeUrl;
  });
  vi.stubGlobal("speechSynthesis", synthesis);
  vi.stubGlobal("SpeechSynthesisUtterance", TestUtterance);
  let resolveAudio!: (audio: Blob) => void;
  vi.mocked(requestAudio).mockReturnValue(new Promise<Blob>((resolve) => {
    resolveAudio = resolve;
  }));
  return {
    stopPlayback: engine === "edge" ? audio.pause : synthesis.cancel,
    async finishLoading() {
      await act(async () => {
        if (engine === "edge") {
          resolveAudio(new Blob(["audio"], { type: "audio/mpeg" }));
        } else {
          const utterance = synthesis.speak.mock.lastCall![0];
          utterance.onstart?.(createSpeechEvent(utterance));
        }
      });
    },
    expectPendingOrPlaying(label: string) {
      expect(screen.getByRole("button", { name: label }).getAttribute("aria-pressed"))
        .toBe("true");
      expect(audio.pause).not.toHaveBeenCalled();
      expect(revokeUrl).not.toHaveBeenCalled();
      expect(synthesis.cancel).not.toHaveBeenCalled();
      if (engine === "edge") {
        expect(requestAudio).toHaveBeenCalledOnce();
        expect(vi.mocked(requestAudio).mock.calls[0][1].signal?.aborted).toBe(false);
      } else {
        expect(synthesis.speak).toHaveBeenCalledOnce();
        expect(requestAudio).not.toHaveBeenCalled();
      }
    },
  };
}

describe.each<SpeechSettings["engine"]>(["edge", "web-speech"])("workspace %s speech independence", (engine) => {
  it.each(["loading", "playing"])("keeps %s speech through submission, task routing and translation completion", async (speechStatus) => {
    const state = createWorkspaceState();
    state.settings.speech.engine = engine;
    const speech = mockWorkspaceSpeech(engine);
    const completedTask = { ...createCompletedTask("Translated result"), text: "Hello there" };
    let resolveSubmission!: (task: Task) => void;
    vi.mocked(request)
      .mockReturnValueOnce(new Promise<Task>((resolve) => { resolveSubmission = resolve; }))
      .mockResolvedValueOnce({ ...completedTask, status: "running", translation: "" })
      .mockResolvedValueOnce(completedTask);
    const { source, props, rerender, openTask } = renderWorkspace("", state);
    fireEvent.change(source, { target: { value: completedTask.text } });
    fireEvent.click(screen.getAllByRole("button", { name: "朗读" })[0]);
    if (speechStatus === "playing") await speech.finishLoading();
    const activeLabel = speechStatus === "loading" ? "取消加载" : "停止朗读";
    speech.expectPendingOrPlaying(activeLabel);

    await act(() => vi.advanceTimersByTimeAsync(600));
    expect(screen.getByText("正在提交…")).toBeTruthy();
    speech.expectPendingOrPlaying(activeLabel);
    await act(async () => {
      resolveSubmission({ ...completedTask, status: "queued", translation: "" });
    });
    expect(screen.getByText("任务已进入队列")).toBeTruthy();
    expect(openTask).toHaveBeenCalledExactlyOnceWith(completedTask.id);
    rerender(
      <FeedbackProvider>
        <TranslatePage {...props} taskId={completedTask.id} />
      </FeedbackProvider>,
    );
    speech.expectPendingOrPlaying(activeLabel);
    await act(() => vi.advanceTimersByTimeAsync(700));
    expect(screen.getByText("正在翻译")).toBeTruthy();
    speech.expectPendingOrPlaying(activeLabel);
    await act(() => vi.advanceTimersByTimeAsync(700));
    expect(screen.getByText(completedTask.translation)).toBeTruthy();
    speech.expectPendingOrPlaying(activeLabel);

    rerender(
      <FeedbackProvider>
        <TranslatePage {...props} taskId={completedTask.id} state={structuredClone(state)} />
      </FeedbackProvider>,
    );
    speech.expectPendingOrPlaying(activeLabel);
    if (speechStatus === "loading") await speech.finishLoading();
    speech.expectPendingOrPlaying("停止朗读");
    fireEvent.click(screen.getByRole("button", { name: "停止朗读" }));
    expect(speech.stopPlayback).toHaveBeenCalledOnce();
    expect(screen.queryByRole("button", { name: "停止朗读" })).toBeNull();
    expect(request).toHaveBeenCalledTimes(3);
  });

  it("keeps the original reading through edits and clearing while retaining its stop control", async () => {
    const state = createWorkspaceState();
    state.settings.speech.engine = engine;
    const speech = mockWorkspaceSpeech(engine);
    const task = createCompletedTask("Original translation");
    vi.mocked(request).mockResolvedValueOnce(task);
    renderWorkspace(task.id, state);
    await act(async () => {});
    fireEvent.click(screen.getAllByRole("button", { name: "朗读" })[1]);
    await speech.finishLoading();

    fireEvent.click(screen.getByRole("button", { name: "原文 Markdown 源码" }));
    fireEvent.change(screen.getByRole("textbox", { name: "待翻译原文" }), {
      target: { value: "New source" },
    });
    fireEvent.change(screen.getByRole("combobox", { name: "目标语言" }), {
      target: { value: "en" },
    });
    fireEvent.click(screen.getByRole("button", { name: "清空原文和结果" }));
    speech.expectPendingOrPlaying("停止朗读");
    expect((screen.getByRole("button", { name: "停止朗读" }) as HTMLButtonElement).disabled)
      .toBe(false);
    fireEvent.click(screen.getByRole("button", { name: "停止朗读" }));
    expect(speech.stopPlayback).toHaveBeenCalledOnce();
    expect(screen.getAllByRole("button", { name: "朗读" }).every(
      (button) => (button as HTMLButtonElement).disabled,
    )).toBe(true);
    expect(request).toHaveBeenCalledOnce();
  });

  it("still stops on switching readings, changing speech configuration and leaving the page", async () => {
    const state = createWorkspaceState();
    state.settings.speech.engine = engine;
    const speech = mockWorkspaceSpeech(engine);
    const task = createCompletedTask("Translated result");
    vi.mocked(request).mockResolvedValueOnce(task);
    const { props, rerender, unmount } = renderWorkspace(task.id, state);
    await act(async () => {});
    fireEvent.click(screen.getAllByRole("button", { name: "朗读" })[0]);
    await speech.finishLoading();
    fireEvent.click(screen.getByRole("button", { name: "朗读" }));
    await speech.finishLoading();
    expect(speech.stopPlayback).toHaveBeenCalledOnce();

    const updatedState = structuredClone(state);
    if (engine === "edge") updatedState.settings.speech.edgeVoices.en = "en-US-AriaNeural";
    else updatedState.settings.speech.webSpeechVoices.en = "english";
    rerender(
      <FeedbackProvider>
        <TranslatePage {...props} state={updatedState} />
      </FeedbackProvider>,
    );
    expect(speech.stopPlayback).toHaveBeenCalledTimes(2);
    expect(screen.queryByRole("button", { name: "停止朗读" })).toBeNull();
    fireEvent.click(screen.getAllByRole("button", { name: "朗读" })[0]);
    await speech.finishLoading();
    unmount();
    expect(speech.stopPlayback).toHaveBeenCalledTimes(3);
  });
});

describe("workspace event wiring", () => {
  const scrollIntoViewDescriptor = Object.getOwnPropertyDescriptor(
    Element.prototype,
    "scrollIntoView",
  );
  beforeEach(() => {
    // JSDOM has no layout scrolling; retain the real Radix focus lifecycle.
    Object.defineProperty(Element.prototype, "scrollIntoView", {
      configurable: true,
      value: vi.fn(),
    });
  });
  afterEach(() => {
    if (scrollIntoViewDescriptor) {
      Object.defineProperty(
        Element.prototype,
        "scrollIntoView",
        scrollIntoViewDescriptor,
      );
    } else {
      Reflect.deleteProperty(Element.prototype, "scrollIntoView");
    }
  });

  it("persists only the chosen provider and waits for saving before opening settings", async () => {
    const state = createWorkspaceState();
    state.providers.push({ ...state.providers[0], id: "second-provider", name: "Second service" });
    const originalSettings = structuredClone(state.settings);
    const savedSettings = { ...originalSettings, defaultProviderId: "second-provider" };
    let finishSave!: (settings: Settings) => void;
    const pendingSave = new Promise<Settings>((resolve) => { finishSave = resolve; });
    vi.mocked(request).mockImplementation(async (path) => {
      if (path === "/state") return state;
      if (path === "/settings") return pendingSave;
      throw new Error(`Unexpected request: ${path}`);
    });
    let application!: ReturnType<typeof render>;
    await act(async () => {
      application = render(<FeedbackProvider><App /></FeedbackProvider>);
    });
    await act(async () => vi.dynamicImportSettled());
    fireEvent.change(screen.getByRole("combobox", { name: "目标语言" }), {
      target: { value: "en" },
    });
    const providerSelect = screen.getByRole("combobox", { name: "翻译服务" }) as HTMLButtonElement;
    fireEvent.keyDown(providerSelect, { key: "Enter" });
    await act(() => vi.advanceTimersByTimeAsync(0));
    fireEvent.click(screen.getByRole("option", { name: "Second service" }));
    expect(providerSelect.textContent).toContain("Second service");
    expect(providerSelect.disabled).toBe(true);
    expect(request).toHaveBeenNthCalledWith(2, "/settings", {
      method: "PUT",
      body: savedSettings,
      signal: expect.any(AbortSignal),
    });
    const saveSignal = vi.mocked(request).mock.calls[1][1]?.signal;
    fireEvent.click(screen.getByRole("link", { name: "应用设置" }));
    expect(saveSignal?.aborted).toBe(false);
    expect(screen.queryByLabelText("默认服务")).toBeNull();
    expect(screen.getByText("正在加载…")).toBeTruthy();
    await act(async () => finishSave(savedSettings));
    expect((screen.getByLabelText("默认服务") as HTMLSelectElement).value)
      .toBe("second-provider");
    expect((screen.getByLabelText("默认目标语言") as HTMLSelectElement).value)
      .toBe(originalSettings.targetLanguage);
    expect(screen.queryByText("有尚未保存的修改")).toBeNull();
    expect(state.settings).toEqual(originalSettings);
    expect(screen.getByText("默认服务已更新")).toBeTruthy();

    fireEvent.click(screen.getByRole("link", { name: "翻译工作台" }));
    expect(screen.getByRole("combobox", { name: "翻译服务" }).textContent)
      .toContain("Second service");
    await act(() => vi.advanceTimersByTimeAsync(1000));
    expect(request).toHaveBeenCalledTimes(2);
    application.unmount();
    vi.mocked(request).mockResolvedValueOnce({ ...state, settings: savedSettings });
    await act(async () => {
      render(<FeedbackProvider><App /></FeedbackProvider>);
    });
    expect(screen.getByRole("combobox", { name: "翻译服务" }).textContent)
      .toContain("Second service");
    expect(request).toHaveBeenCalledTimes(3);
  });

  it("reports failed default saves without retrying and allows an explicit retry", async () => {
    const state = createWorkspaceState();
    state.providers.push({ ...state.providers[0], id: "second-provider", name: "Second service" });
    vi.mocked(request)
      .mockResolvedValueOnce(state)
      .mockRejectedValueOnce(new Error("Settings are unavailable"));
    await act(async () => {
      render(<FeedbackProvider><App /></FeedbackProvider>);
    });
    await act(async () => vi.dynamicImportSettled());
    const providerSelect = screen.getByRole("combobox", { name: "翻译服务" }) as HTMLButtonElement;
    fireEvent.keyDown(providerSelect, { key: "Enter" });
    await act(() => vi.advanceTimersByTimeAsync(0));
    await act(async () => {
      fireEvent.click(screen.getByRole("option", { name: "Second service" }));
    });
    expect(providerSelect.disabled).toBe(false);
    expect(providerSelect.textContent).toContain("Second service");
    expect(screen.getByRole("alert").textContent).toContain("默认服务未保存");
    expect(screen.queryByText("默认服务已更新")).toBeNull();
    await act(() => vi.advanceTimersByTimeAsync(2000));
    expect(request).toHaveBeenCalledTimes(2);
    fireEvent.click(screen.getByRole("button", { name: "打开应用设置" }));
    expect((screen.getByLabelText("默认服务") as HTMLSelectElement).value).toBe("provider");

    fireEvent.click(screen.getByRole("link", { name: "翻译工作台" }));
    vi.mocked(request).mockResolvedValueOnce({ ...state.settings, defaultProviderId: "second-provider" });
    fireEvent.keyDown(screen.getByRole("combobox", { name: "翻译服务" }), { key: "Enter" });
    await act(() => vi.advanceTimersByTimeAsync(0));
    await act(async () => {
      fireEvent.click(screen.getByRole("option", { name: "Second service" }));
    });
    expect(screen.queryByRole("alert")).toBeNull();
    expect(screen.getByText("默认服务已更新")).toBeTruthy();
    expect(request).toHaveBeenCalledTimes(3);
  });

  it("does not replace the saved default when opening a task from another provider", async () => {
    const state = createWorkspaceState();
    state.providers.push({ ...state.providers[0], id: "second-provider", name: "Second service" });
    state.settings.defaultProviderId = "second-provider";
    const task = createCompletedTask("Existing translation");
    vi.mocked(request).mockResolvedValueOnce(state).mockResolvedValueOnce(task);
    window.history.replaceState(null, "", "?task=completed");
    await act(async () => {
      render(<FeedbackProvider><App /></FeedbackProvider>);
    });
    await act(async () => vi.dynamicImportSettled());
    expect(screen.getByRole("combobox", { name: "翻译服务" }).textContent)
      .toContain("Test service");
    expect(screen.getByText("Existing translation")).toBeTruthy();
    await act(() => vi.advanceTimersByTimeAsync(1000));
    expect(request).toHaveBeenCalledTimes(2);
    expect(request).toHaveBeenLastCalledWith("/tasks/completed", { signal: expect.any(AbortSignal) });
    fireEvent.click(screen.getByRole("link", { name: "应用设置" }));
    expect((screen.getByLabelText("默认服务") as HTMLSelectElement).value)
      .toBe("second-provider");
  });

  it("puts fallback provider and prompt choices in the footer without placeholder options", async () => {
    const state = createTestAppState();
    state.providers = [
      {
        id: "first-provider",
        name: "First provider",
        type: "openai-compatible",
        baseUrl: "https://example.test/v1",
        model: "gpt-6-luna",
        timeoutSeconds: 30,
        enabled: true,
        hasApiKey: true,
      },
    ];
    state.providerTypes = [
      {
        type: "openai-compatible",
        name: "OpenAI compatible",
        defaultModel: "gpt-6-luna",
        supportsPrompts: true,
        supportsApiKey: true,
        configurableEndpoint: true,
        requiresModel: true,
      },
    ];
    state.prompts = [
      {
        id: "first-prompt",
        name: "General translation",
        systemTemplate: "Translate.",
        userTemplate: "{{text}}",
        persona: "Serious developer",
        revision: 2,
        builtIn: true,
      },
    ];
    state.settings.defaultProviderId = "missing-provider";
    state.settings.defaultPromptId = "missing-prompt";
    const saveDefaultProvider = vi.fn();
    const { container } = render(
      <FeedbackProvider>
        <TranslatePage
          state={state}
          taskId=""
          openTask={vi.fn()}
          configure={vi.fn()}
          saveDefaultProvider={saveDefaultProvider}
          savingDefaultProvider={false}
        />
      </FeedbackProvider>,
    );
    const providerSelect = screen.getByRole("combobox", {
      name: "翻译服务",
    });
    const promptSelect = screen.getByRole("combobox", {
      name: "提示词模板",
    });
    expect(providerSelect.textContent).toContain("First provider · gpt-6-luna");
    expect(promptSelect.textContent).toContain("General translation");
    const footer = container.querySelector(".translation-actions");
    expect(footer?.contains(providerSelect)).toBe(true);
    expect(footer?.contains(promptSelect)).toBe(true);
    expect(container.querySelectorAll(".card")).toHaveLength(1);
    expect(screen.getByText("自动翻译 · 可能计费")).toBeTruthy();

    act(() => providerSelect.focus());
    fireEvent.keyDown(providerSelect, { key: "ArrowDown" });
    await act(() => vi.advanceTimersByTimeAsync(0));
    expect(screen.getByRole("listbox", { name: "翻译服务" })).toBeTruthy();
    expect(screen.getAllByRole("option")).toHaveLength(1);
    const providerOption = screen.getByRole("option", { name: /First provider/ });
    expect(providerOption.getAttribute("aria-selected")).toBe("true");
    expect(providerOption.textContent).toContain("gpt-6-luna");
    fireEvent.keyDown(providerOption, { key: "Escape" });
    await act(() => vi.advanceTimersByTimeAsync(0));
    expect(screen.queryByRole("listbox")).toBeNull();
    expect(document.activeElement).toBe(providerSelect);

    fireEvent.keyDown(promptSelect, { key: "Enter" });
    await act(() => vi.advanceTimersByTimeAsync(0));
    expect(screen.getAllByRole("option")).toHaveLength(1);
    const promptOption = screen.getByRole("option", { name: "General translation" });
    expect(promptOption.getAttribute("aria-selected")).toBe("true");
    fireEvent.keyDown(promptOption, { key: "Escape" });
    await act(() => vi.advanceTimersByTimeAsync(1000));
    expect(request).not.toHaveBeenCalled();
    expect(saveDefaultProvider).not.toHaveBeenCalled();
  });

  it("debounces footer translation while requesting persistence only for provider changes", async () => {
    const state = createWorkspaceState();
    state.providers.push({
      ...state.providers[0],
      id: "model-provider",
      name: "Model service",
      type: "openai-compatible",
      model: "gpt-6-luna",
    }, {
      ...state.providers[0],
      id: "disabled-provider",
      name: "Disabled service",
      enabled: false,
    });
    state.providerTypes.push({
      type: "openai-compatible",
      name: "OpenAI compatible",
      defaultModel: "gpt-6-luna",
      supportsPrompts: true,
      supportsApiKey: true,
      configurableEndpoint: true,
      requiresModel: true,
    });
    state.prompts = ["General", "Developer"].map((name) => ({
      id: name.toLowerCase(),
      name,
      systemTemplate: "Translate.",
      userTemplate: "{{text}}",
      persona: "",
      revision: 1,
      builtIn: false,
    }));
    state.settings.defaultPromptId = "general";
    const originalSettings = structuredClone(state.settings);
    vi.mocked(request).mockReturnValue(new Promise(() => {}));
    const { source, props } = renderWorkspace("", state);
    expect(screen.queryByRole("combobox", { name: "提示词模板" })).toBeNull();
    fireEvent.change(source, { target: { value: "Hello" } });
    const providerSelect = screen.getByRole("combobox", { name: "翻译服务" });
    fireEvent.blur(source, { relatedTarget: providerSelect });
    fireEvent.keyDown(providerSelect, { key: "Enter" });
    await act(() => vi.advanceTimersByTimeAsync(0));
    expect(request).not.toHaveBeenCalled();
    expect(screen.queryByRole("option", { name: "Disabled service" })).toBeNull();
    fireEvent.click(screen.getByRole("option", { name: /Model service/ }));
    await act(() => vi.advanceTimersByTimeAsync(0));
    expect(props.saveDefaultProvider).toHaveBeenCalledExactlyOnceWith("model-provider");
    const promptSelect = screen.getByRole("combobox", { name: "提示词模板" });
    expect(promptSelect.textContent).toContain("General");
    fireEvent.keyDown(promptSelect, { key: "Enter" });
    await act(() => vi.advanceTimersByTimeAsync(0));
    fireEvent.keyDown(screen.getByRole("listbox"), { key: "End" });
    await act(() => vi.advanceTimersByTimeAsync(0));
    const developerOption = screen.getByRole("option", { name: "Developer" });
    expect(document.activeElement).toBe(developerOption);
    fireEvent.keyDown(developerOption, { key: "Enter" });
    await act(() => vi.advanceTimersByTimeAsync(599));
    expect(request).not.toHaveBeenCalled();
    expect(props.saveDefaultProvider).toHaveBeenCalledOnce();
    await act(() => vi.advanceTimersByTimeAsync(1));
    expect(request).toHaveBeenCalledExactlyOnceWith("/tasks", expect.objectContaining({
      method: "POST",
      body: expect.objectContaining({
        text: "Hello",
        providerId: "model-provider",
        promptId: "developer",
      }),
    }));

    fireEvent.keyDown(providerSelect, { key: "Enter" });
    await act(() => vi.advanceTimersByTimeAsync(0));
    fireEvent.click(screen.getByRole("option", { name: "Test service" }));
    expect(screen.queryByRole("combobox", { name: "提示词模板" })).toBeNull();
    expect(screen.queryByText(/可能计费/)).toBeNull();
    await act(() => vi.advanceTimersByTimeAsync(600));
    expect(request).toHaveBeenCalledTimes(2);
    expect(request).toHaveBeenLastCalledWith("/tasks", expect.objectContaining({
      body: expect.objectContaining({ providerId: "provider", promptId: "" }),
    }));
    expect(props.saveDefaultProvider).toHaveBeenCalledTimes(2);
    expect(props.saveDefaultProvider).toHaveBeenLastCalledWith("provider");
    expect(state.settings).toEqual(originalSettings);
  });

  it("disables unavailable and loading services while preserving configuration recovery", () => {
    const emptyWorkspace = renderWorkspace("", createTestAppState());
    const emptySelect = screen.getByRole("combobox", { name: "翻译服务" });
    expect((emptySelect as HTMLButtonElement).disabled).toBe(true);
    expect(emptySelect.textContent).toContain("未选择服务");
    fireEvent.click(screen.getByRole("button", { name: "配置服务 →" }));
    expect(emptyWorkspace.props.configure).toHaveBeenCalledOnce();
    expect(request).not.toHaveBeenCalled();
    emptyWorkspace.unmount();

    vi.mocked(request).mockReturnValue(new Promise(() => {}));
    renderWorkspace("loading-task");
    expect((screen.getByRole("combobox", { name: "翻译服务" }) as HTMLButtonElement)
      .disabled).toBe(true);
    expect(request).toHaveBeenCalledExactlyOnceWith("/tasks/loading-task", {
      signal: expect.any(AbortSignal),
    });
  });

  it("adapts density at Unicode character boundaries and restores it when text shrinks", () => {
    const { source, container } = renderWorkspace();
    const columns = container.querySelector(".editor-columns")!;
    expect(columns.getAttribute("data-text-density")).toBe("regular");
    for (const [characterCount, density] of [
      [3000, "regular"], [3001, "compact"], [10000, "compact"],
      [10001, "dense"], [20001, "dense"], [3000, "regular"], [0, "regular"],
    ] as const) {
      const text = "\u{20000}".repeat(characterCount);
      fireEvent.change(source, { target: { value: text } });
      expect(columns.getAttribute("data-text-density")).toBe(density);
      expect(source.value).toBe(text);
      expect(screen.getByRole("textbox", { name: "待翻译原文" })).toBe(source);
    }
    expect(request).not.toHaveBeenCalled();
  });

  it.each(["source", "translation"])("uses the longer %s for both panes without resubmitting on mode changes", async (longerPane) => {
    const task = createCompletedTask(longerPane === "translation" ? "中".repeat(10001) : "Short result");
    task.text = longerPane === "source" ? "text ".repeat(2001) : "Short source";
    vi.mocked(request).mockResolvedValueOnce(task);
    const { container } = renderWorkspace(task.id);
    await act(async () => { await Promise.resolve(); });
    const columns = container.querySelector(".editor-columns")!;
    expect(columns.getAttribute("data-text-density")).toBe("dense");
    expect(columns.querySelectorAll(".markdown-prose")).toHaveLength(2);
    fireEvent.click(screen.getByRole("button", { name: "译文 Markdown 源码" }));
    expect(columns.getAttribute("data-text-density")).toBe("dense");
    expect((screen.getByRole("textbox", { name: "待翻译原文" }) as HTMLTextAreaElement).value)
      .toBe(task.text);
    expect(columns.querySelector(".preserve-text")!.textContent).toBe(task.translation);
    fireEvent.click(screen.getByRole("button", { name: "译文 Markdown 排版" }));
    expect(columns.getAttribute("data-text-density")).toBe("dense");
    await act(() => vi.advanceTimersByTimeAsync(1000));
    expect(request).toHaveBeenCalledExactlyOnceWith(`/tasks/${task.id}`, {
      signal: expect.any(AbortSignal),
    });
    fireEvent.click(screen.getByRole("button", { name: "清空原文和结果" }));
    expect(columns.getAttribute("data-text-density")).toBe("regular");
  });

  it("switches from source to directly editable rich text without submitting", async () => {
    const { source } = renderWorkspace();
    const markdown = "# Heading\n\n**Important** text";
    fireEvent.change(source, { target: { value: markdown } });
    const modeButton = screen.getByRole("button", { name: "原文 Markdown 排版" });
    fireEvent.blur(source, { relatedTarget: modeButton });
    fireEvent.click(modeButton);
    await act(() => vi.advanceTimersByTimeAsync(0));

    expect(screen.getByRole("heading", { name: "Heading" })).toBeTruthy();
    expect(screen.getByText("Important").tagName).toBe("STRONG");
    expect(screen.getByRole("textbox", { name: "待翻译原文" })
      .getAttribute("contenteditable")).toBe("true");
    expect(request).not.toHaveBeenCalled();
  });

  it("opens clipboard tasks in Markdown and preserves text when either shared mode switch is used", async () => {
    const task = createCompletedTask("## 译文标题\n\n**译文：**中文结果\n\n- 第一项\n- 第二项");
    task.text = "# Source heading\n\n**现在可用的方法：**复制实际的 Markdown 源码。";
    task.entry = "clipboard-ui";
    vi.mocked(request).mockResolvedValueOnce(task);
    renderWorkspace("completed");
    await act(async () => {
      await Promise.resolve();
    });

    const translationMarkdownButton = screen.getByRole("button", {
      name: "译文 Markdown 排版",
    });
    const translationSourceButton = screen.getByRole("button", {
      name: "译文 Markdown 源码",
    });
    expect(translationMarkdownButton.getAttribute("aria-pressed")).toBe("true");
    expect(translationSourceButton.getAttribute("aria-pressed")).toBe("false");
    expect(screen.getByText("现在可用的方法：").tagName).toBe("STRONG");
    expect(screen.getByText("译文：").tagName).toBe("STRONG");
    expect(screen.getByRole("heading", { name: "Source heading" })).toBeTruthy();
    expect(
      screen.getByRole("heading", { level: 2, name: "译文标题" }),
    ).toBeTruthy();
    expect(screen.getByText("第一项").tagName).toBe("LI");
    expect(screen.queryByRole("toolbar")).toBeNull();

    const sourceMarkdownButton = screen.getByRole("button", {
      name: "原文 Markdown 排版",
    });
    const sourceCodeButton = screen.getByRole("button", {
      name: "原文 Markdown 源码",
    });
    expect(sourceMarkdownButton.getAttribute("aria-pressed")).toBe("true");
    const originalEditor = screen.getByRole("textbox", { name: "待翻译原文" });
    fireEvent.click(sourceMarkdownButton);
    expect(screen.getByRole("textbox", { name: "待翻译原文" })).toBe(originalEditor);
    fireEvent.click(sourceCodeButton);
    expect(sourceCodeButton.getAttribute("aria-pressed")).toBe("true");
    expect(sourceMarkdownButton.getAttribute("aria-pressed")).toBe("false");
    expect((screen.getByRole("textbox", { name: "待翻译原文" }) as HTMLTextAreaElement)
      .value).toBe(task.text);
    expect(translationMarkdownButton.getAttribute("aria-pressed")).toBe("false");
    expect(translationSourceButton.getAttribute("aria-pressed")).toBe("true");
    expect(screen.getByText(/## 译文标题/)).toBeTruthy();
    fireEvent.click(sourceMarkdownButton);
    expect(screen.getByRole("heading", { name: "Source heading" })).toBeTruthy();
    expect(screen.getByRole("heading", { name: "译文标题" })).toBeTruthy();

    fireEvent.click(translationSourceButton);
    expect(translationMarkdownButton.getAttribute("aria-pressed")).toBe("false");
    expect(translationSourceButton.getAttribute("aria-pressed")).toBe("true");
    expect(sourceCodeButton.getAttribute("aria-pressed")).toBe("true");
    expect((screen.getByRole("textbox", { name: "待翻译原文" }) as HTMLTextAreaElement)
      .value).toBe(task.text);
    expect(screen.getByText(/## 译文标题/)).toBeTruthy();
    expect(screen.queryByRole("heading", { name: "译文标题" })).toBeNull();
    fireEvent.click(translationMarkdownButton);
    expect(screen.getByRole("heading", { name: "译文标题" })).toBeTruthy();
    expect(screen.getByRole("heading", { name: "Source heading" })).toBeTruthy();
    expect(screen.getByText("现在可用的方法：").tagName).toBe("STRONG");
    expect(screen.getByText("译文：").tagName).toBe("STRONG");
    await act(() => vi.advanceTimersByTimeAsync(1000));
    expect(request).toHaveBeenCalledExactlyOnceWith("/tasks/completed", {
      signal: expect.any(AbortSignal),
    });

    fireEvent.click(screen.getByRole("button", { name: "清空原文和结果" }));
    await act(() => vi.advanceTimersByTimeAsync(1000));
    expect(screen.getByRole("textbox", { name: "待翻译原文" }).textContent).toBe("");
    expect(screen.getByText("暂无译文")).toBeTruthy();
    expect(request).toHaveBeenCalledOnce();
  });

  it("keeps unsupported source intact and synchronizes the fallback mode", async () => {
    const task = createCompletedTask("# Translated");
    task.text = "![Private](https://example.invalid/image.png)";
    vi.mocked(request).mockResolvedValueOnce(task);
    const { container } = renderWorkspace("completed");
    await act(async () => { await Promise.resolve(); });

    expect((screen.getByRole("textbox", { name: "待翻译原文" }) as HTMLTextAreaElement)
      .value).toBe(task.text);
    expect(screen.getByRole("button", { name: "原文 Markdown 源码" })
      .getAttribute("aria-pressed")).toBe("true");
    expect(screen.getByRole("button", { name: "译文 Markdown 源码" })
      .getAttribute("aria-pressed")).toBe("true");
    expect(container.querySelector("img")).toBeNull();
    await act(() => vi.advanceTimersByTimeAsync(1000));
    expect(request).toHaveBeenCalledOnce();
  });

  it("shows reused results immediately without polling or resubmitting unchanged input", async () => {
    const task = { ...createCompletedTask("Reused translation"), reusedFromTaskId: "previous-task" };
    vi.mocked(request).mockResolvedValueOnce(task);
    const { source } = renderWorkspace();
    fireEvent.change(source, { target: { value: task.text } });
    await act(() => vi.advanceTimersByTimeAsync(600));

    expect(screen.getByText("已复用")).toBeTruthy();
    expect(screen.getByText("Reused translation")).toBeTruthy();
    expect(screen.queryByRole("button", { name: "取消任务" })).toBeNull();
    fireEvent.blur(source);
    fireEvent.keyDown(source, { key: "Enter", ctrlKey: true });
    await act(() => vi.advanceTimersByTimeAsync(2100));
    expect(request).toHaveBeenCalledExactlyOnceWith("/tasks", expect.objectContaining({ method: "POST" }));
  });

  it("uses composition-aware automatic translation without a submit button and ignores clear-button blur", async () => {
    vi.mocked(request).mockReturnValue(new Promise(() => {}));
    const { source } = renderWorkspace();
    expect(screen.queryByRole("button", { name: "开始翻译" })).toBeNull();
    fireEvent.compositionStart(source);
    fireEvent.change(source, { target: { value: "ni" } });
    fireEvent.blur(source);
    await act(() => vi.advanceTimersByTimeAsync(1000));
    expect(request).not.toHaveBeenCalled();
    fireEvent.change(source, { target: { value: "你好" } });
    fireEvent.compositionEnd(source);
    fireEvent.blur(source);
    await act(() => vi.advanceTimersByTimeAsync(0));
    expect(request).toHaveBeenCalledExactlyOnceWith(
      "/tasks",
      expect.objectContaining({
        method: "POST",
        body: expect.objectContaining({
          text: "你好",
          targetLanguage: "auto",
          promptId: "",
        }),
      }),
    );
    expect(source.disabled).toBe(false);
    fireEvent.change(source, { target: { value: "Changed" } });
    const clear = screen.getByRole("button", { name: "清空原文和结果" });
    fireEvent.blur(source, { relatedTarget: clear });
    fireEvent.click(clear);
    await act(() => vi.advanceTimersByTimeAsync(1000));
    expect(request).toHaveBeenCalledOnce();
    expect(source.value).toBe("");
  });

  it.each(["en", "zh-CN"])("immediately switches target %s to auto when source detection is selected", async (initialTargetLanguage) => {
    const state = createWorkspaceState();
    state.settings.sourceLanguage = initialTargetLanguage === "en" ? "zh-CN" : "en";
    state.settings.targetLanguage = initialTargetLanguage;
    const originalSettings = structuredClone(state.settings);
    vi.mocked(request).mockReturnValue(new Promise(() => {}));
    const { source } = renderWorkspace("", state);
    const sourceSelect = screen.getByRole("combobox", { name: "源语言" }) as HTMLSelectElement;
    const targetSelect = screen.getByRole("combobox", { name: "目标语言" }) as HTMLSelectElement;

    fireEvent.change(source, { target: { value: "Hello" } });
    fireEvent.blur(source, { relatedTarget: sourceSelect });
    fireEvent.change(sourceSelect, { target: { value: "auto" } });

    expect(sourceSelect.value).toBe("auto");
    expect(targetSelect.value).toBe("auto");
    expect(state.settings).toEqual(originalSettings);
    await act(() => vi.advanceTimersByTimeAsync(599));
    expect(request).not.toHaveBeenCalled();
    await act(() => vi.advanceTimersByTimeAsync(1));
    expect(request).toHaveBeenCalledExactlyOnceWith("/tasks", expect.objectContaining({
      method: "POST",
      body: expect.objectContaining({
        text: "Hello",
        sourceLanguage: "auto",
        targetLanguage: "auto",
      }),
    }));
  });

  it("preserves a manually selected target when editing text or selecting a concrete source", async () => {
    vi.mocked(request).mockReturnValue(new Promise(() => {}));
    const { source } = renderWorkspace();
    const sourceSelect = screen.getByRole("combobox", { name: "源语言" }) as HTMLSelectElement;
    const targetSelect = screen.getByRole("combobox", { name: "目标语言" }) as HTMLSelectElement;

    fireEvent.change(targetSelect, { target: { value: "en" } });
    fireEvent.change(source, { target: { value: "你好" } });
    expect(sourceSelect.value).toBe("auto");
    expect(targetSelect.value).toBe("en");
    fireEvent.change(sourceSelect, { target: { value: "zh-CN" } });
    expect(targetSelect.value).toBe("en");
    await act(() => vi.advanceTimersByTimeAsync(600));
    expect(request).toHaveBeenCalledExactlyOnceWith("/tasks", expect.objectContaining({
      method: "POST",
      body: expect.objectContaining({
        text: "你好",
        sourceLanguage: "zh-CN",
        targetLanguage: "en",
      }),
    }));
  });

  it("keeps automatic language choices when its own task URL is reflected by the router", async () => {
    const { source, props, rerender, openTask } = renderWorkspace();
    const task: Task = {
      id: "created",
      text: "Hello",
      translation: "Result",
      sourceLanguage: "en",
      targetLanguage: "zh-CN",
      status: "succeeded",
      entry: "web",
      providerSnapshot: props.state.providers[0],
      promptSnapshot: {
        id: "",
        name: "",
        persona: "",
        revision: 0,
        builtIn: false,
        systemTemplate: "",
        userTemplate: "",
      },
      errorCode: "",
      errorMessage: "",
      durationMs: 20,
      createdAt: "",
      updatedAt: "",
      pasteStatus: "",
    };
    vi.mocked(request).mockResolvedValueOnce(task);
    fireEvent.change(source, { target: { value: "Hello" } });
    await act(() => vi.advanceTimersByTimeAsync(600));
    expect(openTask).toHaveBeenCalledExactlyOnceWith("created");
    rerender(
      <FeedbackProvider>
        <TranslatePage {...props} taskId="created" />
      </FeedbackProvider>,
    );
    expect(
      (screen.getByRole("combobox", { name: "目标语言" }) as HTMLSelectElement)
        .value,
    ).toBe("auto");
    fireEvent.blur(source);
    await act(() => vi.advanceTimersByTimeAsync(1000));
    expect(request).toHaveBeenCalledOnce();
    expect(screen.getByText("Result")).toBeTruthy();
  });
});

describe("shared confirmation and toast", () => {
  it("requires explicit confirmation, defaults to cancellation and returns focus", async () => {
    const remove = vi.fn();
    function ConfirmationExample() {
      const { confirm, notify } = useFeedback();
      const [count, setCount] = useState(0);
      return (
        <button
          onClick={async () => {
            if (
              !(await confirm({
                title: "Delete item?",
                description: "This cannot be undone.",
                confirmLabel: "Delete",
                danger: true,
              }))
            )
              return;
            remove();
            setCount((current) => current + 1);
            notify("Deleted");
          }}
        >
          Remove {count}
        </button>
      );
    }
    render(
      <FeedbackProvider>
        <ConfirmationExample />
      </FeedbackProvider>,
    );
    const trigger = screen.getByRole("button", { name: "Remove 0" });
    act(() => trigger.focus());
    fireEvent.click(trigger);
    expect(screen.getByRole("alertdialog")).toBeTruthy();
    expect(document.activeElement).toBe(
      screen.getByRole("button", { name: "取消" }),
    );
    expect(remove).not.toHaveBeenCalled();
    fireEvent.keyDown(document.activeElement!, { key: "Escape" });
    await act(() => vi.advanceTimersByTimeAsync(0));
    expect(screen.queryByRole("alertdialog")).toBeNull();
    expect(document.activeElement).toBe(trigger);
    expect(remove).not.toHaveBeenCalled();
    fireEvent.click(trigger);
    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: "Delete" }));
    });
    expect(remove).toHaveBeenCalledOnce();
    expect(screen.queryByRole("alertdialog")).toBeNull();
    expect(screen.getByText("Deleted")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "关闭通知" }));
    expect(screen.queryByText("Deleted")).toBeNull();
  });
});
