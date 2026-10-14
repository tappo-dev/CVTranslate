import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  AutoTranslationController,
  initialTranslationState,
} from "./auto-translation";
import type { Task, TranslationInput } from "./types";

const initialInput: TranslationInput = {
  text: "",
  sourceLanguage: "auto",
  targetLanguage: "auto",
  providerId: "provider",
  promptId: "",
  entry: "web",
};

function createTask(id = "task", overrides: Partial<Task> = {}): Task {
  return {
    id,
    text: "Hello",
    translation: "",
    sourceLanguage: "en",
    targetLanguage: "zh-CN",
    entry: "web",
    status: "queued",
    errorCode: "",
    errorMessage: "",
    pasteStatus: "",
    createdAt: "",
    updatedAt: "",
    durationMs: 0,
    providerSnapshot: {
      id: "provider",
      name: "Test",
      type: "microsoft-translate",
      baseUrl: "",
      model: "",
      enabled: true,
      hasApiKey: false,
      timeoutSeconds: 30,
    },
    promptSnapshot: {
      id: "",
      name: "",
      systemTemplate: "",
      userTemplate: "",
      persona: "",
      revision: 0,
      builtIn: false,
    },
    ...overrides,
  };
}

function deferred<Value>() {
  let resolve!: (value: Value) => void;
  let reject!: (error: unknown) => void;
  const promise = new Promise<Value>((complete, fail) => {
    resolve = complete;
    reject = fail;
  });
  return { promise, resolve, reject };
}

function setup() {
  const dependencies = {
    create: vi
      .fn<(input: TranslationInput, signal: AbortSignal) => Promise<Task>>()
      .mockResolvedValue(createTask()),
    load: vi
      .fn<(taskId: string, signal: AbortSignal) => Promise<Task>>()
      .mockResolvedValue(
        createTask("task", { status: "succeeded", translation: "Result" }),
      ),
    cancel: vi
      .fn<(taskId: string, signal: AbortSignal) => Promise<Task>>()
      .mockResolvedValue(createTask("task", { status: "cancelled" })),
  };
  let state = initialTranslationState(initialInput);
  const changed = vi.fn((updated: typeof state) => {
    state = updated;
  });
  const navigate = vi.fn();
  const controller = new AutoTranslationController(
    initialInput,
    dependencies,
    changed,
    navigate,
  );
  function edit(
    text: string,
    changes: Partial<TranslationInput> = {},
    configured = true,
  ) {
    controller.update({ ...state.input, text, ...changes }, configured);
  }
  return {
    controller,
    dependencies,
    changed,
    navigate,
    edit,
    getState: () => state,
  };
}

beforeEach(() => vi.useFakeTimers());
afterEach(() => {
  vi.clearAllTimers();
  vi.useRealTimers();
});

describe("automatic translation lifecycle", () => {
  it("debounces typing and deduplicates blur, shortcut and timer submissions", async () => {
    const { controller, dependencies, edit, getState } = setup();
    edit("H");
    await vi.advanceTimersByTimeAsync(400);
    edit("Hello");
    await vi.advanceTimersByTimeAsync(599);
    expect(dependencies.create).not.toHaveBeenCalled();
    controller.flush();
    controller.flush();
    await vi.advanceTimersByTimeAsync(1);
    expect(dependencies.create).toHaveBeenCalledExactlyOnceWith(
      { ...initialInput, text: "Hello" },
      expect.any(AbortSignal),
    );
    await vi.advanceTimersByTimeAsync(700);
    expect(getState().task?.translation).toBe("Result");
    expect(getState().input.targetLanguage).toBe("auto");
    controller.flush();
    await vi.advanceTimersByTimeAsync(3000);
    expect(dependencies.create).toHaveBeenCalledOnce();
    expect(dependencies.load).toHaveBeenCalledOnce();
  });

  it("waits for IME completion even on blur and submits only committed text", async () => {
    const { controller, dependencies, edit } = setup();
    controller.setComposing(true);
    edit("ni");
    controller.flush();
    await vi.advanceTimersByTimeAsync(2000);
    expect(dependencies.create).not.toHaveBeenCalled();
    edit("\u4f60\u597d");
    controller.setComposing(false);
    edit("\u4f60\u597d");
    await vi.advanceTimersByTimeAsync(600);
    expect(dependencies.create).toHaveBeenCalledExactlyOnceWith(
      { ...initialInput, text: "\u4f60\u597d" },
      expect.any(AbortSignal),
    );
  });

  it("does not submit empty, oversized or unconfigured input", async () => {
    const { controller, dependencies, edit } = setup();
    for (const text of ["  ", "\u{1f600}".repeat(20001)]) {
      edit(text);
      controller.flush();
      await vi.advanceTimersByTimeAsync(1000);
    }
    edit("Hello", {}, false);
    controller.flush();
    await vi.advanceTimersByTimeAsync(1000);
    expect(dependencies.create).not.toHaveBeenCalled();
    edit("\u{1f600}".repeat(20000));
    await vi.advanceTimersByTimeAsync(600);
    expect(dependencies.create).toHaveBeenCalledOnce();
  });

  it("cancels late obsolete acknowledgements without replacing the latest input or task URL", async () => {
    const { controller, dependencies, edit, getState, navigate } = setup();
    const first = deferred<Task>();
    dependencies.create.mockReturnValueOnce(first.promise);
    edit("Old");
    controller.flush();
    edit("New", {
      providerId: "other",
      targetLanguage: "en",
      promptId: "prompt",
    });
    dependencies.create.mockResolvedValueOnce(createTask("new"));
    await vi.advanceTimersByTimeAsync(600);
    first.resolve(createTask("old"));
    await vi.advanceTimersByTimeAsync(0);
    expect(dependencies.cancel).toHaveBeenCalledExactlyOnceWith(
      "old",
      expect.any(AbortSignal),
    );
    expect(getState().input.text).toBe("New");
    expect(getState().task?.id).toBe("new");
    expect(navigate).toHaveBeenCalledExactlyOnceWith("new");
  });

  it("ignores late polling results after editing and cancels only its own obsolete job", async () => {
    const { dependencies, edit, getState } = setup();
    const response = deferred<Task>();
    dependencies.load.mockReturnValueOnce(response.promise);
    edit("Old");
    await vi.advanceTimersByTimeAsync(1300);
    const signal = dependencies.load.mock.calls[0][1];
    edit("New");
    expect(signal.aborted).toBe(true);
    response.resolve(
      createTask("task", { status: "succeeded", translation: "Stale" }),
    );
    await vi.advanceTimersByTimeAsync(0);
    expect(getState().task).toBeNull();
    expect(getState().input.text).toBe("New");
    expect(dependencies.cancel).toHaveBeenCalledOnce();
  });

  it("clears pending input and permits intentionally re-entering previously submitted text", async () => {
    const { controller, dependencies, edit, getState } = setup();
    edit("Hello");
    edit("");
    await vi.advanceTimersByTimeAsync(1000);
    expect(dependencies.create).not.toHaveBeenCalled();
    edit("Hello");
    await vi.advanceTimersByTimeAsync(600);
    edit("");
    expect(getState().task).toBeNull();
    expect(dependencies.cancel).toHaveBeenCalledOnce();
    edit("Hello");
    controller.flush();
    expect(dependencies.create).toHaveBeenCalledTimes(2);
  });

  it("loads native task links without resubmission or implicit native-job cancellation", async () => {
    const { controller, dependencies, getState, edit } = setup();
    dependencies.load.mockResolvedValueOnce(
      createTask("native", { entry: "clipboard-paste", status: "running" }),
    );
    controller.open("native");
    await vi.advanceTimersByTimeAsync(0);
    controller.flush();
    expect(dependencies.create).not.toHaveBeenCalled();
    expect(getState().input.text).toBe("Hello");
    edit("Changed");
    await vi.advanceTimersByTimeAsync(600);
    expect(dependencies.cancel).not.toHaveBeenCalled();
    expect(dependencies.create).toHaveBeenCalledOnce();
  });

  it("does not retry failed submissions on blur, but supports an explicit retry", async () => {
    const { controller, dependencies, getState, edit } = setup();
    dependencies.create.mockRejectedValueOnce(new Error("Failed"));
    edit("Hello");
    await vi.advanceTimersByTimeAsync(600);
    expect(getState().error).toBe("Failed");
    controller.flush();
    await vi.advanceTimersByTimeAsync(5000);
    expect(dependencies.create).toHaveBeenCalledOnce();
    controller.retry();
    await vi.advanceTimersByTimeAsync(0);
    expect(dependencies.create).toHaveBeenCalledTimes(2);
    expect(getState().error).toBe("");
  });

  it("refreshes failed polls without creating another translation", async () => {
    const { controller, dependencies, getState, edit } = setup();
    dependencies.load.mockRejectedValueOnce(new Error("Offline"));
    edit("Hello");
    await vi.advanceTimersByTimeAsync(4000);
    expect(getState().pollError).toBe("Offline");
    expect(dependencies.load).toHaveBeenCalledOnce();
    controller.refresh();
    await vi.advanceTimersByTimeAsync(0);
    expect(getState().pollError).toBe("");
    expect(getState().task?.status).toBe("succeeded");
    expect(dependencies.create).toHaveBeenCalledOnce();
  });

  it("cancels a submitting job once its acknowledgement arrives and does not restart on blur", async () => {
    const { controller, dependencies, edit, getState } = setup();
    const response = deferred<Task>();
    dependencies.create.mockReturnValueOnce(response.promise);
    edit("Hello");
    controller.flush();
    await controller.cancel();
    response.resolve(createTask());
    await vi.advanceTimersByTimeAsync(0);
    expect(dependencies.cancel).toHaveBeenCalledOnce();
    controller.flush();
    await vi.advanceTimersByTimeAsync(2000);
    expect(getState().task).toBeNull();
    expect(getState().cancelled).toBe(true);
    expect(dependencies.create).toHaveBeenCalledOnce();
  });

  it("cancels an active task without accepting a stale poll result", async () => {
    const { controller, dependencies, edit, getState } = setup();
    const response = deferred<Task>();
    dependencies.load.mockReturnValueOnce(response.promise);
    edit("Hello");
    await vi.advanceTimersByTimeAsync(1300);
    await controller.cancel();
    response.resolve(createTask("task", { status: "running" }));
    await vi.advanceTimersByTimeAsync(0);
    expect(getState().task?.status).toBe("cancelled");
    controller.flush();
    expect(dependencies.create).toHaveBeenCalledOnce();
  });

  it("finishes an explicitly requested cancellation after disposal and a late acknowledgement", async () => {
    const { controller, dependencies, changed, navigate, edit } = setup();
    const response = deferred<Task>();
    const cancellation = deferred<Task>();
    dependencies.create.mockReturnValueOnce(response.promise);
    dependencies.cancel.mockReturnValueOnce(cancellation.promise);
    edit("Hello");
    controller.flush();
    await controller.cancel();
    controller.dispose();
    const changeCount = changed.mock.calls.length;
    expect(dependencies.create.mock.calls[0][1].aborted).toBe(false);
    response.resolve(createTask("pending-cancel"));
    await vi.advanceTimersByTimeAsync(0);
    expect(dependencies.cancel).toHaveBeenCalledExactlyOnceWith(
      "pending-cancel",
      expect.any(AbortSignal),
    );
    expect(dependencies.cancel.mock.calls[0][1].aborted).toBe(false);
    cancellation.resolve(createTask("pending-cancel", { status: "cancelled" }));
    await vi.advanceTimersByTimeAsync(2000);
    expect(changed).toHaveBeenCalledTimes(changeCount);
    expect(navigate).not.toHaveBeenCalled();
    expect(dependencies.load).not.toHaveBeenCalled();
  });

  it("does not abort an in-flight explicit cancellation when leaving the workspace", async () => {
    const { controller, dependencies, changed, navigate, edit } = setup();
    const cancellation = deferred<Task>();
    dependencies.cancel.mockReturnValueOnce(cancellation.promise);
    edit("Hello");
    await vi.advanceTimersByTimeAsync(600);
    const operation = controller.cancel();
    controller.dispose();
    const changeCount = changed.mock.calls.length;
    const navigationCount = navigate.mock.calls.length;
    expect(dependencies.cancel.mock.calls[0][1].aborted).toBe(false);
    cancellation.resolve(createTask("task", { status: "cancelled" }));
    await operation;
    await vi.advanceTimersByTimeAsync(2000);
    expect(changed).toHaveBeenCalledTimes(changeCount);
    expect(navigate).toHaveBeenCalledTimes(navigationCount);
    expect(dependencies.load).not.toHaveBeenCalled();
  });

  it("retains the current submission error when an older cancellation later fails", async () => {
    const { dependencies, edit, getState } = setup();
    const cancellation = deferred<Task>();
    dependencies.cancel.mockReturnValueOnce(cancellation.promise);
    edit("Old");
    await vi.advanceTimersByTimeAsync(600);
    dependencies.create.mockRejectedValueOnce(
      new Error("Provider rejected the new request"),
    );
    edit("New");
    await vi.advanceTimersByTimeAsync(600);
    cancellation.reject(new Error("Cancellation failed"));
    await vi.advanceTimersByTimeAsync(0);
    expect(getState().error).toBe("Provider rejected the new request");
    expect(getState().cancellationWarning?.taskId).toBe("task");
    expect(getState().cancellationWarning?.message).not.toBe("");
  });

  it("does not carry an old cancellation warning into a newly opened task", async () => {
    const { controller, dependencies, edit, getState } = setup();
    const cancellation = deferred<Task>();
    dependencies.cancel.mockReturnValueOnce(cancellation.promise);
    edit("Old");
    await vi.advanceTimersByTimeAsync(600);
    edit("New");
    dependencies.load.mockResolvedValueOnce(
      createTask("history-task", { status: "succeeded" }),
    );
    controller.open("history-task");
    await vi.advanceTimersByTimeAsync(0);
    cancellation.reject(new Error("Cancellation failed"));
    await vi.advanceTimersByTimeAsync(0);
    expect(getState().task?.id).toBe("history-task");
    expect(getState().error).toBe("");
    expect(getState().cancellationWarning).toBeNull();
  });

  it("reports an unacknowledged cancelled submission separately without retrying it", async () => {
    const { controller, dependencies, edit, getState } = setup();
    const response = deferred<Task>();
    dependencies.create.mockReturnValueOnce(response.promise);
    edit("Hello");
    controller.flush();
    await controller.cancel();
    response.reject(new Error("Acknowledgement lost"));
    await vi.advanceTimersByTimeAsync(2000);
    expect(getState().cancellationWarning?.taskId).toBe("");
    expect(getState().cancellationWarning?.message).not.toBe("");
    expect(getState().error).toBe("");
    expect(dependencies.create).toHaveBeenCalledOnce();
    expect(dependencies.cancel).not.toHaveBeenCalled();
  });

  it("suppresses late cleanup failures after disposal", async () => {
    const { controller, dependencies, changed, edit } = setup();
    const cancellation = deferred<Task>();
    dependencies.cancel.mockReturnValueOnce(cancellation.promise);
    edit("Old");
    await vi.advanceTimersByTimeAsync(600);
    edit("");
    controller.dispose();
    const changeCount = changed.mock.calls.length;
    cancellation.reject(new Error("Cancellation failed"));
    await vi.advanceTimersByTimeAsync(2000);
    expect(changed).toHaveBeenCalledTimes(changeCount);
    expect(dependencies.cancel).toHaveBeenCalledOnce();
  });

  it("stops timers and ignores late responses on disposal without a cancellation request", async () => {
    const { controller, dependencies, changed, navigate, edit } = setup();
    const response = deferred<Task>();
    dependencies.create.mockReturnValueOnce(response.promise);
    edit("Hello");
    controller.flush();
    const signal = dependencies.create.mock.calls[0][1];
    controller.dispose();
    const changeCount = changed.mock.calls.length;
    response.resolve(createTask());
    await vi.advanceTimersByTimeAsync(5000);
    expect(signal.aborted).toBe(true);
    expect(changed).toHaveBeenCalledTimes(changeCount);
    expect(navigate).not.toHaveBeenCalled();
    expect(dependencies.load).not.toHaveBeenCalled();
    expect(dependencies.cancel).not.toHaveBeenCalled();
  });
});
