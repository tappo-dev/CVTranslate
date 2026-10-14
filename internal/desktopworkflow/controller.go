// Package desktopworkflow coordinates native actions with the shared job service.
package desktopworkflow

import (
	"context"
	"errors"
	"sync"
	"time"

	"cvtranslate/internal/domain"
	"cvtranslate/internal/platform"
)

type desktop interface {
	CaptureClipboard() (platform.ClipboardCapture, error)
	Paste(context.Context, platform.ClipboardCapture, string) error
}

type jobs interface {
	Wait(context.Context, string) (domain.Task, error)
}

type history interface {
	UpdatePasteStatus(context.Context, string, string) error
}

// ShortcutStatus is transient diagnostic metadata, never captured or translated text.
type ShortcutStatus struct {
	Action     string    `json:"action"`
	Stage      string    `json:"stage"`
	Message    string    `json:"message"`
	OccurredAt time.Time `json:"occurredAt"`
	TaskID     string    `json:"taskId,omitempty"`
}

type Controller struct {
	desktop        desktop
	jobs           jobs
	history        history
	start          func(domain.TranslationInput) (domain.Task, error)
	open           func(string) error
	context        context.Context
	cancel         context.CancelFunc
	mutex          sync.Mutex
	workers        sync.WaitGroup
	closed         bool
	busy           bool
	status         ShortcutStatus
	generation     uint64
	busyGeneration uint64
}

func New(native desktop, tasks jobs, records history, start func(domain.TranslationInput) (domain.Task, error), open func(string) error) *Controller {
	workflowContext, cancel := context.WithCancel(context.Background())
	return &Controller{desktop: native, jobs: tasks, history: records, start: start, open: open, context: workflowContext, cancel: cancel}
}

func (controller *Controller) Message() string {
	return controller.Status().Message
}

func (controller *Controller) Status() ShortcutStatus {
	controller.mutex.Lock()
	defer controller.mutex.Unlock()
	return controller.status
}

// beginStatus requires mutex ownership. Reports from superseded invocations are ignored.
func (controller *Controller) beginStatus(action string) func(string, string, string) {
	controller.generation++
	generation := controller.generation
	controller.status = ShortcutStatus{Action: action, Stage: "received", Message: "Shortcut received.", OccurredAt: time.Now()}
	return func(stage, message, taskID string) {
		controller.mutex.Lock()
		defer controller.mutex.Unlock()
		if generation != controller.generation {
			return
		}
		controller.status.Stage = stage
		controller.status.Message = message
		controller.status.OccurredAt = time.Now()
		if taskID != "" {
			controller.status.TaskID = taskID
		}
	}
}

func validAction(action string) bool {
	switch action {
	case "open-ui", "translate-paste", "translate-ui":
		return true
	default:
		return false
	}
}

func captureFailureMessage(action string, err error) string {
	detail := platform.DiagnosticMessage(err)
	if detail != "" {
		return "Translation was not started: " + detail
	}
	return "Translation was not started: clipboard text unavailable, changed or unsafe to capture. Copy plain text again and retry the shortcut."
}

func (controller *Controller) execute(action string, captured *platform.ClipboardCapture) {
	controller.mutex.Lock()
	if controller.closed || !validAction(action) {
		controller.mutex.Unlock()
		return
	}
	report := controller.beginStatus(action)
	controller.mutex.Unlock()
	controller.executeReported(action, captured, report)
}

func (controller *Controller) Handle(event platform.ShortcutEvent) {
	action := event.Action
	controller.mutex.Lock()
	if controller.closed || !validAction(action) {
		controller.mutex.Unlock()
		return
	}
	if action != "open-ui" && controller.busy {
		// A repeat belongs to neither a new invocation nor a concurrently opened UI.
		if controller.generation == controller.busyGeneration {
			controller.status.Stage = "busy"
			controller.status.Message = "A shortcut translation is already in progress. Repeated shortcut ignored."
			controller.status.OccurredAt = time.Now()
		}
		controller.mutex.Unlock()
		return
	}
	report := controller.beginStatus(action)
	if event.CaptureError != nil {
		controller.mutex.Unlock()
		report("capture_failed", captureFailureMessage(action, event.CaptureError), "")
		return
	}
	if action != "open-ui" {
		controller.busy = true
		controller.busyGeneration = controller.generation
	}
	controller.workers.Add(1)
	controller.mutex.Unlock()
	go func() {
		defer controller.workers.Done()
		if action != "open-ui" {
			defer func() { controller.mutex.Lock(); controller.busy = false; controller.mutex.Unlock() }()
		}
		controller.executeReported(action, event.Capture, report)
	}()
}

func (controller *Controller) executeReported(action string, captured *platform.ClipboardCapture, report func(string, string, string)) {
	if controller.context.Err() != nil {
		report("cancelled", "Shortcut cancelled; nothing was pasted.", "")
		return
	}
	if action == "open-ui" {
		if err := controller.open(""); err != nil {
			report("open_failed", "Could not open the browser. Reopen the application.", "")
		} else {
			report("opened", "Browser open requested.", "")
		}
		return
	}
	pasteResult := action == "translate-paste"
	if action != "translate-paste" && action != "translate-ui" {
		return
	}
	var capture platform.ClipboardCapture
	report("capturing", "Reading the shortcut's captured text and paste target.", "")
	if captured != nil {
		capture = *captured
	} else {
		var err error
		capture, err = controller.desktop.CaptureClipboard()
		if err != nil {
			report("capture_failed", captureFailureMessage(action, err), "")
			return
		}
	}
	entry := "clipboard-ui"
	if pasteResult {
		entry = "clipboard-paste"
	}
	if controller.context.Err() != nil {
		report("cancelled", "Shortcut cancelled; nothing was pasted.", "")
		return
	}
	report("starting", "Submitting the translation using the default provider and prompt.", "")
	task, err := controller.start(domain.TranslationInput{Text: capture.Text, Entry: entry})
	if err != nil {
		report("start_failed", "Translation could not start. Check the default provider and prompt in Settings.", "")
		return
	}
	if pasteResult && capture.UsesDirectPaste() {
		report("direct_translating", "Experimental three-copy action: translation will be pasted into whichever window is active when it completes, without target or selection verification. Release modifiers and avoid copying other text.", task.ID)
	} else {
		report("translating", "Translation is running. Keep the original input and clipboard unchanged.", task.ID)
	}
	if !pasteResult {
		if err := controller.open(task.ID); err != nil {
			report("open_failed", "The browser could not open. The translation is available in History.", "")
		} else {
			report("opened", "Translation submitted and browser open requested. Follow its progress in History.", "")
		}
		return
	}
	task, err = controller.jobs.Wait(controller.context, task.ID)
	if controller.context.Err() != nil || errors.Is(err, context.Canceled) {
		report("cancelled", "Shortcut cancelled; nothing was pasted.", "")
		return
	}
	if err != nil {
		report("translation_failed", "Translation could not be observed; nothing was pasted. See History.", "")
		return
	}
	if task.Status == "cancelled" {
		report("cancelled", "Translation cancelled; nothing was pasted.", "")
		return
	}
	if task.Status != "succeeded" {
		report("translation_failed", "Translation did not complete; nothing was pasted. See History.", "")
		return
	}
	pasteStatus := "dispatched"
	if capture.UsesDirectPaste() {
		report("direct_pasting", "Experimental direct paste: checking clipboard and modifiers only, then sending the platform paste shortcut without verifying the active target.", "")
	} else {
		report("pasting", "Checking the original target and clipboard before automatic paste.", "")
	}
	if err := controller.desktop.Paste(controller.context, capture, task.Translation); err != nil {
		pasteStatus = "skipped"
		stage := "paste_skipped"
		message := "Automatic paste was skipped because the target, clipboard or input state could not be verified. Copy the result from History."
		if capture.UsesDirectPaste() {
			stage = "direct_paste_skipped"
			message = "Experimental direct paste did not complete. Check clipboard, modifier and task state; the original target was not verified. Copy the result from History."
		}
		if detail := platform.DiagnosticMessage(err); detail != "" {
			message = "Automatic paste was skipped: " + detail + " Copy the result from History."
		}
		report(stage, message, "")
	} else {
		if capture.UsesDirectPaste() {
			report("direct_paste_dispatched", "Experimental paste was dispatched to the active window without target verification. The clipboard contains the translation; insertion is not confirmed.", "")
		} else {
			report("paste_dispatched", "Paste was dispatched. The clipboard now contains the translation; insertion is not independently confirmed.", "")
		}
	}
	if err := controller.history.UpdatePasteStatus(context.Background(), task.ID, pasteStatus); err != nil {
		report("outcome_failed", "Translation completed, but the paste outcome could not be saved.", "")
	}
}

func (controller *Controller) Close() {
	controller.mutex.Lock()
	controller.closed = true
	controller.cancel()
	controller.mutex.Unlock()
	controller.workers.Wait()
}
