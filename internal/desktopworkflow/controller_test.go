package desktopworkflow

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"cvtranslate/internal/domain"
	"cvtranslate/internal/platform"
)

type fakeDesktop struct {
	pasteCalls   int
	pasteError   error
	captureCalls int
	pastedSource string
}

func (native *fakeDesktop) CaptureClipboard() (platform.ClipboardCapture, error) {
	native.captureCalls++
	return platform.ClipboardCapture{Text: "source"}, nil
}
func (native *fakeDesktop) Paste(_ context.Context, capture platform.ClipboardCapture, _ string) error {
	native.pasteCalls++
	native.pastedSource = capture.Text
	return native.pasteError
}

type completedJob struct{ status string }

func (job completedJob) Wait(ctx context.Context, identifier string) (domain.Task, error) {
	if err := ctx.Err(); err != nil {
		return domain.Task{}, err
	}
	return domain.Task{ID: identifier, Status: job.status, Translation: "result"}, nil
}

type fakeHistory struct {
	outcome     string
	updateError error
}

func (records *fakeHistory) UpdatePasteStatus(_ context.Context, _, outcome string) error {
	records.outcome = outcome
	return records.updateError
}

func TestOnlyCompletedTranslationCanDispatchPaste(t *testing.T) {
	for _, status := range []string{"failed", "cancelled", "interrupted", "succeeded"} {
		t.Run(status, func(t *testing.T) {
			native := &fakeDesktop{}
			records := &fakeHistory{}
			controller := New(native, completedJob{status}, records, func(input domain.TranslationInput) (domain.Task, error) {
				if input.Entry != "clipboard-paste" || input.Text != "source" {
					t.Fatal("incorrect native task input")
				}
				return domain.Task{ID: "task"}, nil
			}, func(string) error { t.Fatal("paste path opened browser"); return nil })
			defer controller.Close()
			controller.execute("translate-paste", nil)
			if status == "succeeded" {
				if native.pasteCalls != 1 || records.outcome != "dispatched" {
					t.Fatal("completed job not dispatched")
				}
			} else if native.pasteCalls != 0 {
				t.Fatal("non-successful job pasted")
			}
		})
	}
}

func TestUnsafePasteIsRecordedAsSkipped(t *testing.T) {
	native := &fakeDesktop{pasteError: errors.New("private native error text")}
	records := &fakeHistory{}
	controller := New(native, completedJob{"succeeded"}, records, func(domain.TranslationInput) (domain.Task, error) { return domain.Task{ID: "task"}, nil }, func(string) error { return nil })
	defer controller.Close()
	controller.execute("translate-paste", nil)
	if records.outcome != "skipped" {
		t.Fatal("unsafe paste was not marked skipped")
	}
	status := controller.Status()
	if status.Stage != "paste_skipped" || status.TaskID != "task" || status.Action != "translate-paste" || strings.Contains(status.Message, "private native error text") {
		t.Fatalf("incorrect or unsafe paste status: %+v", status)
	}
}

func TestShutdownInvalidatesPasteAndDisplayDoesNotPaste(t *testing.T) {
	native := &fakeDesktop{}
	openedTask := ""
	controller := New(native, completedJob{"succeeded"}, &fakeHistory{}, func(domain.TranslationInput) (domain.Task, error) { return domain.Task{ID: "task"}, nil }, func(identifier string) error { openedTask = identifier; return nil })
	controller.execute("translate-ui", nil)
	if openedTask != "task" || native.pasteCalls != 0 {
		t.Fatal("display workflow changed destination")
	}
	controller.cancel()
	controller.execute("translate-paste", nil)
	if native.pasteCalls != 0 {
		t.Fatal("shutdown did not invalidate paste")
	}
	controller.Close()
}

func TestRepeatedCopyUsesBoundCaptureWithoutReadingLaterClipboard(test *testing.T) {
	native := &fakeDesktop{}
	var translatedText string
	controller := New(native, completedJob{"succeeded"}, &fakeHistory{}, func(input domain.TranslationInput) (domain.Task, error) {
		translatedText = input.Text
		return domain.Task{ID: "task"}, nil
	}, func(string) error { return nil })
	defer controller.Close()
	captured := &platform.ClipboardCapture{Text: "gesture clipboard"}
	controller.execute("translate-ui", captured)
	if translatedText != "gesture clipboard" || native.captureCalls != 0 || native.pasteCalls != 0 {
		test.Fatal("repeated shortcut recaptured unrelated clipboard content or changed its action")
	}
}

type countedJobs struct{ waitCalls int }

func (tasks *countedJobs) Wait(_ context.Context, identifier string) (domain.Task, error) {
	tasks.waitCalls++
	return domain.Task{ID: identifier, Status: "succeeded", Translation: "translated"}, nil
}

func TestClipboardTranslationActionsUseCorrectDestination(test *testing.T) {
	for _, scenario := range []struct {
		action string
		entry  string
		paste  bool
	}{
		{"translate-paste", "clipboard-paste", true},
		{"translate-ui", "clipboard-ui", false},
	} {
		test.Run(scenario.action, func(test *testing.T) {
			native := &fakeDesktop{}
			tasks := &countedJobs{}
			records := &fakeHistory{}
			started, opened := false, false
			controller := New(native, tasks, records, func(input domain.TranslationInput) (domain.Task, error) {
				if input.Text != "source" || input.Entry != scenario.entry || opened {
					test.Fatalf("wrong source/entry or opened before starting: %+v", input)
				}
				started = true
				return domain.Task{ID: "pending-task", Status: "queued"}, nil
			}, func(identifier string) error {
				if !started || identifier != "pending-task" || tasks.waitCalls != 0 {
					test.Fatal("UI must open the already-submitted task without waiting for completion")
				}
				opened = true
				return nil
			})
			defer controller.Close()
			controller.execute(scenario.action, nil)
			if !started || native.captureCalls != 1 {
				test.Fatal("source was mixed or a translation was not submitted")
			}
			if scenario.paste {
				if opened || native.pasteCalls != 1 || native.pastedSource != "source" || tasks.waitCalls != 1 || records.outcome != "dispatched" {
					test.Fatal("paste action did not wait and paste against its captured source")
				}
			} else if !opened || native.pasteCalls != 0 || tasks.waitCalls != 0 {
				test.Fatal("UI action waited or pasted")
			}
		})
	}
}

func TestCaptureErrorStatusIsSafeAndInvalidActionsLeaveItUnchanged(test *testing.T) {
	native := &fakeDesktop{}
	tasks := &countedJobs{}
	controller := New(native, tasks, &fakeHistory{}, func(domain.TranslationInput) (domain.Task, error) {
		test.Fatal("capture failure must not submit a task")
		return domain.Task{}, nil
	}, func(string) error { test.Fatal("capture failure must not open a browser"); return nil })
	defer controller.Close()
	for _, action := range []string{"translate-paste", "translate-ui"} {
		controller.Handle(platform.ShortcutEvent{Action: action, Capture: &platform.ClipboardCapture{Text: "private source"}, CaptureError: errors.New("private native details")})
		status := controller.Status()
		if status.Action != action || status.Stage != "capture_failed" || status.TaskID != "" || status.OccurredAt.IsZero() || controller.Message() != status.Message {
			test.Fatalf("missing capture failure metadata: %+v", status)
		}
		encoded, err := json.Marshal(status)
		if err != nil || strings.Contains(string(encoded), "private") || strings.Contains(string(encoded), "taskId") {
			test.Fatalf("unsafe status encoding: %s (%v)", encoded, err)
		}
		controller.Handle(platform.ShortcutEvent{Action: "invalid", CaptureError: errors.New("invalid")})
		if controller.Status() != status {
			test.Fatal("invalid event replaced the failure diagnostic")
		}
	}
	if native.captureCalls != 0 || tasks.waitCalls != 0 {
		test.Fatal("failed bound capture performed another capture or waited for a job")
	}
}

func TestUIActionsReportSubmittedTranslationBeforeOpening(test *testing.T) {
	for _, action := range []string{"translate-ui"} {
		test.Run(action, func(test *testing.T) {
			tasks := &countedJobs{}
			var controller *Controller
			controller = New(&fakeDesktop{}, tasks, &fakeHistory{}, func(domain.TranslationInput) (domain.Task, error) {
				if controller.Status().Stage != "starting" {
					test.Fatal("submission did not report starting")
				}
				return domain.Task{ID: "submitted-task"}, nil
			}, func(identifier string) error {
				status := controller.Status()
				if status.Stage != "translating" || status.TaskID != identifier || identifier != "submitted-task" || tasks.waitCalls != 0 {
					test.Fatalf("browser opened before submitted translation status: %+v", status)
				}
				return nil
			})
			defer controller.Close()
			controller.execute(action, nil)
			if status := controller.Status(); status.Stage != "opened" || status.Action != action || status.TaskID != "submitted-task" {
				test.Fatalf("incorrect opened status: %+v", status)
			}
		})
	}
}

func TestSupersededInvocationCannotOverwriteNewStatus(test *testing.T) {
	controller := New(&fakeDesktop{}, &countedJobs{}, &fakeHistory{}, nil, nil)
	defer controller.Close()
	for _, nextAction := range []string{"open-ui", "translate-paste"} {
		controller.mutex.Lock()
		oldReport := controller.beginStatus("translate-paste")
		currentReport := controller.beginStatus(nextAction)
		controller.mutex.Unlock()
		currentReport("open_failed", "current message", "current-task")
		expected := controller.Status()
		oldReport("paste_dispatched", "outdated message", "outdated-task")
		if controller.Status() != expected {
			test.Fatal("superseded invocation overwrote a newer action or generation")
		}
	}
}

type waitingJobs struct {
	entered chan struct{}
	release chan struct{}
}

func (tasks *waitingJobs) Wait(ctx context.Context, identifier string) (domain.Task, error) {
	close(tasks.entered)
	select {
	case <-tasks.release:
		return domain.Task{ID: identifier, Status: "succeeded", Translation: "result"}, nil
	case <-ctx.Done():
		return domain.Task{}, ctx.Err()
	}
}

func TestBusyRepeatPreservesOwnerAndConcurrentOpenStatus(test *testing.T) {
	for _, openDuringTranslation := range []bool{false, true} {
		tasks := &waitingJobs{entered: make(chan struct{}), release: make(chan struct{})}
		controller := New(&fakeDesktop{}, tasks, &fakeHistory{}, func(domain.TranslationInput) (domain.Task, error) {
			return domain.Task{ID: "owner-task"}, nil
		}, func(string) error { return nil })
		controller.Handle(platform.ShortcutEvent{Action: "translate-paste"})
		<-tasks.entered
		controller.Handle(platform.ShortcutEvent{Action: "translate-ui"})
		status := controller.Status()
		if status.Stage != "busy" || status.Action != "translate-paste" || status.TaskID != "owner-task" {
			test.Errorf("repeat replaced the owner's identity: %+v", status)
		}
		if openDuringTranslation {
			controller.execute("open-ui", nil)
			status = controller.Status()
			controller.Handle(platform.ShortcutEvent{Action: "translate-ui"})
			if controller.Status() != status {
				test.Error("busy repeat was attributed to concurrently opened UI")
			}
		}
		close(tasks.release)
		controller.workers.Wait()
		if openDuringTranslation {
			if controller.Status() != status {
				test.Error("older translation overwrote newer open-UI status")
			}
		} else if status := controller.Status(); status.Stage != "paste_dispatched" || status.Action != "translate-paste" || status.TaskID != "owner-task" {
			test.Errorf("worker result was attributed to the repeated action: %+v", status)
		}
		controller.Close()
	}
}

func TestCloseCancelsWaitingShortcutWithoutPasting(test *testing.T) {
	native := &fakeDesktop{}
	tasks := &waitingJobs{entered: make(chan struct{}), release: make(chan struct{})}
	controller := New(native, tasks, &fakeHistory{}, func(domain.TranslationInput) (domain.Task, error) {
		return domain.Task{ID: "cancelled-task"}, nil
	}, func(string) error { return nil })
	controller.Handle(platform.ShortcutEvent{Action: "translate-paste"})
	<-tasks.entered
	controller.Close()
	status := controller.Status()
	if status.Stage != "cancelled" || status.TaskID != "cancelled-task" || native.pasteCalls != 0 || controller.busy {
		test.Fatalf("shutdown did not cancel and release the workflow: %+v", status)
	}
	controller.Handle(platform.ShortcutEvent{Action: "open-ui"})
	if controller.Status() != status {
		test.Fatal("closed controller accepted another shortcut")
	}
}

type failedJobs struct{ waitError error }

func (tasks failedJobs) Wait(context.Context, string) (domain.Task, error) {
	return domain.Task{}, tasks.waitError
}

func TestWorkflowFailuresRemainVisibleWithoutRawErrors(test *testing.T) {
	privateError := errors.New("private source or provider details")
	for _, scenario := range []struct {
		name         string
		action       string
		startError   error
		openError    error
		waitError    error
		outcomeError error
		stage        string
	}{
		{name: "submission", action: "translate-ui", startError: privateError, stage: "start_failed"},
		{name: "open result", action: "translate-ui", openError: privateError, stage: "open_failed"},
		{name: "open only", action: "open-ui", openError: privateError, stage: "open_failed"},
		{name: "wait", action: "translate-paste", waitError: privateError, stage: "translation_failed"},
		{name: "wait cancelled", action: "translate-paste", waitError: context.Canceled, stage: "cancelled"},
		{name: "persist outcome", action: "translate-paste", outcomeError: privateError, stage: "outcome_failed"},
	} {
		test.Run(scenario.name, func(test *testing.T) {
			var tasks jobs = completedJob{"succeeded"}
			if scenario.waitError != nil {
				tasks = failedJobs{scenario.waitError}
			}
			native := &fakeDesktop{}
			controller := New(native, tasks, &fakeHistory{updateError: scenario.outcomeError}, func(domain.TranslationInput) (domain.Task, error) {
				return domain.Task{ID: "task"}, scenario.startError
			}, func(string) error {
				if scenario.startError != nil {
					test.Fatal("failed submission opened a browser")
				}
				return scenario.openError
			})
			defer controller.Close()
			controller.execute(scenario.action, nil)
			status := controller.Status()
			if status.Stage != scenario.stage || status.Message == "" || strings.Contains(status.Message, "private") {
				test.Fatalf("incorrect or unsafe failure status: %+v", status)
			}
			if scenario.outcomeError == nil && native.pasteCalls != 0 {
				test.Fatal("failed workflow dispatched a paste")
			}
		})
	}
}
