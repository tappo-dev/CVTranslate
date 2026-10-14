import { errorMessage } from "./api";
import { isActive, type Task, type TranslationInput } from "./types";

interface TranslationDependencies {
  create: (input: TranslationInput, signal: AbortSignal) => Promise<Task>;
  load: (taskId: string, signal: AbortSignal) => Promise<Task>;
  cancel: (taskId: string, signal: AbortSignal) => Promise<Task>;
}

export interface TranslationState {
  input: TranslationInput;
  task: Task | null;
  waiting: boolean;
  submitting: boolean;
  loading: boolean;
  cancelling: boolean;
  cancelled: boolean;
  error: string;
  pollError: string;
  cancellationWarning: { taskId: string; message: string } | null;
}

export function initialTranslationState(
  input: TranslationInput,
): TranslationState {
  return {
    input,
    task: null,
    waiting: false,
    submitting: false,
    loading: false,
    cancelling: false,
    cancelled: false,
    error: "",
    pollError: "",
    cancellationWarning: null,
  };
}

export class AutoTranslationController {
  private state: TranslationState;
  private generation = 0;
  private navigationGeneration = 0;
  private disposed = false;
  private composing = false;
  private configured = false;
  private attemptedInput = "";
  private linkedTaskId = "";
  private ownsTask = false;
  private debounceTimer: ReturnType<typeof setTimeout> | undefined;
  private pollTimer: ReturnType<typeof setTimeout> | undefined;
  private reader: AbortController | null = null;
  private pendingSubmissions = new Map<
    AbortController,
    { cancellationRequested: boolean }
  >();

  constructor(
    input: TranslationInput,
    private dependencies: TranslationDependencies,
    private onChange: (state: TranslationState) => void,
    private onTaskLink: (taskId: string) => void,
  ) {
    this.state = initialTranslationState(input);
  }

  private publish(changes: Partial<TranslationState>) {
    if (this.disposed) return;
    this.state = { ...this.state, ...changes };
    this.onChange(this.state);
  }

  private invalidate() {
    this.generation += 1;
    clearTimeout(this.debounceTimer);
    clearTimeout(this.pollTimer);
    this.reader?.abort();
    this.reader = null;
  }

  private link(taskId: string) {
    if (taskId === this.linkedTaskId) return;
    this.linkedTaskId = taskId;
    this.onTaskLink(taskId);
  }

  private canSubmit() {
    const { input } = this.state;
    return (
      this.configured &&
      !this.composing &&
      !!input.text.trim() &&
      Array.from(input.text).length <= 20000 &&
      JSON.stringify(input) !== this.attemptedInput
    );
  }

  private cancelPendingSubmissions() {
    for (const submission of this.pendingSubmissions.values()) {
      submission.cancellationRequested = true;
    }
  }

  update(input: TranslationInput, configured: boolean) {
    if (this.disposed) return;
    this.configured = configured;
    if (JSON.stringify(input) === JSON.stringify(this.state.input)) return;
    const obsoleteTask =
      this.ownsTask && isActive(this.state.task) ? this.state.task : null;
    this.cancelPendingSubmissions();
    this.invalidate();
    this.attemptedInput = "";
    this.ownsTask = false;
    this.publish({
      input,
      task: null,
      waiting: false,
      loading: false,
      submitting: false,
      cancelling: false,
      cancelled: false,
      error: "",
      pollError: "",
    });
    this.link("");
    if (obsoleteTask) void this.cancelObsolete(obsoleteTask.id);
    this.schedule();
  }

  setComposing(composing: boolean) {
    this.composing = composing;
    clearTimeout(this.debounceTimer);
    this.publish({ waiting: false });
    if (!composing) this.schedule();
  }

  private schedule() {
    clearTimeout(this.debounceTimer);
    if (!this.canSubmit() || this.disposed) return;
    this.publish({ waiting: true });
    this.debounceTimer = setTimeout(() => this.flush(), 600);
  }

  flush() {
    clearTimeout(this.debounceTimer);
    if (!this.canSubmit() || this.disposed) return;
    const input = { ...this.state.input };
    this.attemptedInput = JSON.stringify(input);
    const generation = this.generation;
    const navigationGeneration = this.navigationGeneration;
    const controller = new AbortController();
    const submission = { cancellationRequested: false };
    this.pendingSubmissions.set(controller, submission);
    this.publish({
      waiting: false,
      submitting: true,
      cancelled: false,
      error: "",
    });
    // Keep the acknowledgement on edits so an obsolete server job can be cancelled by ID.
    void this.dependencies
      .create(input, controller.signal)
      .then((task) => {
        if (submission.cancellationRequested) {
          if (isActive(task))
            void this.cancelObsolete(task.id, navigationGeneration);
          return;
        }
        if (this.disposed) return;
        if (generation !== this.generation) {
          if (isActive(task))
            void this.cancelObsolete(task.id, navigationGeneration);
          return;
        }
        this.ownsTask = true;
        this.publish({ task, submitting: false });
        this.link(task.id);
        this.schedulePoll(task, generation);
      })
      .catch((error: unknown) => {
        if (submission.cancellationRequested) {
          this.warnCancellation("", navigationGeneration);
          return;
        }
        if (generation === this.generation)
          this.publish({ submitting: false, error: errorMessage(error) });
      })
      .finally(() => this.pendingSubmissions.delete(controller));
  }

  retry() {
    if (
      this.disposed ||
      this.state.loading ||
      this.state.submitting ||
      this.state.cancelling ||
      isActive(this.state.task)
    )
      return;
    this.invalidate();
    this.attemptedInput = "";
    this.publish({ task: null, pollError: "" });
    this.link("");
    this.flush();
  }

  private warnCancellation(taskId: string, navigationGeneration: number) {
    if (navigationGeneration !== this.navigationGeneration) return;
    this.publish({
      cancellationWarning: {
        taskId,
        message: "任务取消未确认，可在历史中查看。",
      },
    });
  }

  private async cancelObsolete(
    taskId: string,
    navigationGeneration = this.navigationGeneration,
  ) {
    const controller = new AbortController();
    try {
      await this.dependencies.cancel(taskId, controller.signal);
    } catch {
      this.warnCancellation(taskId, navigationGeneration);
    }
  }

  open(taskId: string) {
    if (this.disposed || taskId === this.linkedTaskId) return;
    this.invalidate();
    this.navigationGeneration += 1;
    this.composing = false;
    this.ownsTask = false;
    this.linkedTaskId = taskId;
    this.publish({
      ...(!taskId ? { input: { ...this.state.input, text: "" } } : {}),
      task: null,
      waiting: false,
      submitting: false,
      cancelling: false,
      cancelled: false,
      loading: !!taskId,
      error: "",
      pollError: "",
      cancellationWarning: null,
    });
    if (taskId) void this.load(taskId, this.generation, true);
  }

  private schedulePoll(task: Task, generation: number) {
    if (!this.disposed && generation === this.generation && isActive(task)) {
      this.pollTimer = setTimeout(() => {
        void this.load(task.id, generation);
      }, 700);
    }
  }

  private async load(taskId: string, generation: number, hydrate = false) {
    const controller = new AbortController();
    this.reader = controller;
    try {
      const task = await this.dependencies.load(taskId, controller.signal);
      if (
        this.disposed ||
        controller.signal.aborted ||
        generation !== this.generation
      )
        return;
      if (hydrate) {
        const input: TranslationInput = {
          text: task.text,
          sourceLanguage: task.sourceLanguage,
          targetLanguage: task.targetLanguage,
          providerId: task.providerSnapshot.id,
          promptId: task.promptSnapshot.id || "",
          entry: "web",
        };
        this.attemptedInput = JSON.stringify(input);
        this.publish({ input });
      }
      this.publish({ task, loading: false, pollError: "" });
      this.schedulePoll(task, generation);
    } catch (error) {
      if (!controller.signal.aborted && generation === this.generation) {
        this.publish({ loading: false, pollError: errorMessage(error) });
      }
    }
  }

  refresh() {
    if (!this.linkedTaskId || this.disposed) return;
    this.invalidate();
    this.publish({ pollError: "", loading: !this.state.task });
    void this.load(this.linkedTaskId, this.generation, !this.state.task);
  }

  async cancel() {
    if (this.disposed || this.state.cancelling) return;
    const task = this.state.task;
    this.cancelPendingSubmissions();
    this.invalidate();
    this.attemptedInput = JSON.stringify(this.state.input);
    const generation = this.generation;
    this.publish({ waiting: false, submitting: false, cancelled: !task });
    if (!task || !isActive(task)) return;
    const controller = new AbortController();
    this.publish({ cancelling: true, error: "" });
    try {
      const cancelled = await this.dependencies.cancel(
        task.id,
        controller.signal,
      );
      if (generation === this.generation) {
        this.publish({ task: cancelled, cancelling: false });
        this.schedulePoll(cancelled, generation);
      }
    } catch (error) {
      if (generation === this.generation) {
        this.publish({ error: errorMessage(error), cancelling: false });
        this.schedulePoll(task, generation);
      }
    }
  }

  dispose() {
    this.disposed = true;
    this.invalidate();
    // A requested cancellation still needs the task ID after an in-app navigation.
    // These acknowledgements and cancellation requests retain the API's finite timeout.
    for (const [controller, submission] of this.pendingSubmissions) {
      if (!submission.cancellationRequested) controller.abort();
    }
    this.pendingSubmissions.clear();
  }
}
