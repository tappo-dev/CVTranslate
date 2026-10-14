# Native desktop integration

## Scope and build prerequisites

`internal/platform` implements Windows amd64/arm64 and macOS desktop integration.
It does not implement mobile, Linux desktop, browser-extension, remote desktop,
service-session, sandbox escape, or elevated-input support. Other OS targets,
Windows 32-bit, and macOS builds with cgo disabled expose explicit unsupported
operations, not working native adapters.

- Windows: Go 1.26.1 and the interactive user's desktop session. The adapter uses
  `x/sys/windows`, Win32, and `golang.design/x/hotkey`; it does not require cgo.
  The 64-bit `INPUT` ABI is shared by amd64 and arm64, with a regression assertion.
- macOS: build **on macOS** with Go 1.26.1, `CGO_ENABLED=1`, Apple's clang and
  Command Line Tools/macOS SDK. Cocoa, ApplicationServices/CoreGraphics, Carbon, Security,
  and libdispatch are required. Objective-C code is isolated behind Darwin tags.
  Windows cross-compilation without the Apple toolchain cannot verify this code.
- No changes to the dependency lockfiles are needed by this adapter. Clipboard
  access is implemented natively rather than through the clipboard library:
  native generation checks and the Windows clipboard lock must cover the actual
  read/write operation, not merely surround separate library calls.

## Exported integration API

```go
func RunMain(run func())
func OnReopen(callback func()) func()
func StartTray() (*Tray, error)
func (*Tray) Close() error
func NewDesktop() *Desktop
func (*Desktop) Status() Status
func (*Desktop) ConfigureHotkeys(keys domain.Hotkeys, callback func(ShortcutEvent)) error
func (*Desktop) CaptureClipboard() (ClipboardCapture, error)
func (*Desktop) Paste(ctx context.Context, capture ClipboardCapture, text string) error
func (*Desktop) Close() error
func OpenBrowser(url string) error

func NewCredentials(namespace string) *Credentials
func (*Credentials) Get(id string) (string, error)
func (*Credentials) Set(id, value string) error
func (*Credentials) Delete(id string) error
func (*Credentials) Has(id string) (bool, error)
```

`Status` has exported fields `OS`, `HotkeysAvailable`, `PasteAvailable`, and
`Message`, serialized as `os`, `hotkeysAvailable`, `pasteAvailable`, and `message`.
Availability includes the fixed copy listener's registration/health, not that
every shortcut is unoccupied or every application is editable. `ClipboardCapture.Text` is the
only public capture field; its private fields bind it to the desktop, clipboard
generation, and target. Do not persist, serialize for replay, or log captures.
`ErrUnsupported` is available for unsupported builds. `ErrHotkeysDegraded` is
returned (possibly joined with other errors) if shortcut rollback fails; inspect
it with `errors.Is` and see the recovery contract below.

Call `platform.RunMain(run)` directly from `main`. On Windows this calls `run`;
on macOS the hotkey library owns the Cocoa main event loop and calls `run` from
a worker goroutine. AX capture and pasteboard content reads/writes/dispatch
synchronously marshal to the native main thread. The copy listener runs on its
own thread and samples only foreground identity and pasteboard generation there.
Do not call blocking adapter operations on
the main Cocoa thread while another worker owns the Desktop lock.

On shutdown, cancel the coordinator's contexts, stop accepting actions, close
the desktop and wait for coordinator work before returning from `run`. `Close`
is idempotent and unregisters shortcuts. It does not join arbitrary application
callbacks already executing. The library's macOS `RunMain` implementation exits
the process when `run` returns; cleanup belongs **inside** `run`, not in a defer
outside `RunMain`.

### Windows system tray

`StartTray` owns a hidden top-level Win32 window, a notification icon and a native
popup menu on a locked OS thread. It creates an owned HICON from an embedded
PNG generated from the shared [SVG source](icons.md), with a `CVTranslate`
tooltip. There is no runtime image file lookup or new runtime dependency.
Click/keyboard activation queues `TrayOpenUI`; the context menu has Chinese
labels for Open management UI and Quit. Quit queues `TrayQuit`, replacing any
pending open and suppressing later actions. Native callbacks never open a browser
or perform application cleanup themselves. The owning composition loop consumes
the read-only `Tray.Events` channel.

Initial and tray browser launches run asynchronously with at most one in-flight
call and one coalesced explicit open request. Quit never joins a stalled shell
activation call, and a failed launch never retries without another user request.

`NOTIFYICON_VERSION_4` enables keyboard activation/context-menu delivery. The
hidden window receives `TaskbarCreated` and restores both the icon and protocol
after Explorer restarts. Startup failure aborts application startup; a message
loop/recovery failure closes the event channel, triggering application shutdown
and diagnostic logging rather than leaving an invisible background process.
`Close` posts teardown to the owning thread, joins it and is idempotent. Cleanup
removes the notification entry, destroys its owned HICON, then removes the menu,
window and class; failures are returned.

The composition root cancels shortcut/paste work before provider and HTTP waits,
unregisters native shortcuts, stops reopen/tray delivery, cancels translations
and drains HTTP, then closes storage and releases instance ownership. Browser
closure does not quit and no HTTP shutdown endpoint is added. `--no-native`
skips tray startup; the version-only path returns before native initialization.
Unsupported builds return an inert tray with nil events; native macOS uses the
menu-bar adapter below.

Windows session-end notifications request shutdown on a best-effort basis only;
the OS can terminate the process before cleanup completes. This does not promise
graceful logoff/OS shutdown or forced-termination behavior. The orderly cleanup
contract applies to the explicit tray Quit action.

Automated coverage uses fake shell operations and queued actions only. Actual
icon visibility (including hidden-icons overflow), keyboard/mouse menus, Explorer
restart, quit during a translation and process/port cleanup require separately
authorized Windows desktop acceptance.

### macOS menu bar

Native macOS `StartTray` installs one retained `NSStatusItem` and menu target
inside the existing `RunMain` lifetime. Creation and removal synchronously marshal
to Cocoa's main thread; call `StartTray`/`Close` from the application worker,
never from a native target or the main Cocoa thread. Calls outside that lifetime
or duplicate starts return errors rather than dispatching into an absent loop.
The composition root owns normal cleanup; `RunMain` also removes any remaining
entry before returning to the library's process-exit path.

The template icon uses an embedded 36px PNG derived from the shared SVG source,
displayed at 18pt with a CVTranslate tooltip and accessibility label. The native
adapter copies the bytes during installation and retains no Go pointer. AppKit
supplies its standard menu-bar tint and native menu interaction; this does not
add a WebUI theme or runtime dependency.
The menu has Chinese Open management UI and Quit labels. It does not inspect
clipboard content, request permissions, change activation policy or replace the
Finder reopen delegate. It remains available when shortcut permissions are denied.

Menu targets only update a native atomic action slot. Open coalesces; Quit takes
precedence, disables further actions and never calls `terminate:`. A Go worker
polls the slot every 50 ms without dispatching to Cocoa and feeds the shared
`Tray.Events` queue. Browser launching and ordered cleanup remain in the
composition root. `Close` stops/joins the poller, stops its ticker and then cancels
menu tracking, detaches the menu and releases the status item/target on the main
thread. Concurrent/repeated closes share one cleanup; pending events are not
replayed. A responsive Cocoa loop is required for installation and removal;
forced termination and OS session ending do not promise graceful cleanup.

Windows-host mock tests exercise event delivery, shutdown joining and resource
ownership only. macOS SDK/cgo compilation and authorized menu-bar visibility,
mouse/keyboard interaction, Finder reopen, denied-permission startup and quit
during translation still require a Mac. No macOS artifact is produced on Windows.

### Finder reopen notifications

Native macOS `RunMain` installs a retained `NSApplicationDelegate` on the Cocoa
thread before invoking `run`. `applicationShouldHandleReopen:hasVisibleWindows:`
only sets a coalesced atomic pending flag for our handler; it performs no browser,
HTTP, credential, or Go callback work on that thread. One early notification can
remain pending until the application registers after its HTTP listener is ready:

```go
cleanup := platform.OnReopen(func() {
    // Handle/report this error without logging task-specific browser URLs.
    _ = application.OpenUI("")
})
defer cleanup()
```

The parent composition owns this wiring; platform installation alone does not
open the Web UI. A background Go worker polls the atomic flag every 100 ms and
serializes callbacks. Multiple events between polls or while a callback executes
coalesce into one pending callback. Polling never queues work onto Cocoa's main
thread. There is no tray, native window, permission request, or startup browser
open in this bridge. Initial application launch remains the existing startup
path; this handler concerns reopening an already-running application.

Register once per `RunMain` lifetime. Nil callbacks, duplicate subscriptions and
calls outside that lifetime return no-op cleanups. Unsubscribe disables native
notification acceptance, stops polling, and joins any callback already running;
none can execute after unsubscribe returns. Call it before shutting down the
HTTP server, not from its own callback or the Cocoa main thread. The callback
must finish promptly: unsubscribe cannot forcibly cancel arbitrary user code.
Unsubscribe is idempotent and intentionally terminal for that run lifetime.

Normal return from `run` also unsubscribes before removing the delegate. MRC
retains/releases keep the wrapper and previous delegate alive while installed.
Unhandled delegate methods and the previous delegate's reopen handler are
forwarded. Teardown restores the previous delegate only if our wrapper is still
installed, preserving a replacement made by another owner. Installation/removal
are fixed-size main-thread operations with no I/O; they still require a live,
responsive Cocoa loop (there is no claimed timeout around dispatch_sync).
`os.Exit`, forced termination, or a stuck main loop cannot run/guarantee normal
teardown. Owners of a pre-existing delegate retain their normal ownership duty
after restoration.

Windows and unsupported builds expose `OnReopen` as a no-op. Their application
entry point retains responsibility for an authenticated second-process reopen.
The macOS delegate bridge is source-implemented but **not native-compiled or
Finder-runtime-tested on this Windows host**. Actual macOS SDK/cgo compilation
and authorized Finder reopen/startup/shutdown checks remain required. Pure-Go
worker tests verify early/coalesced delivery and shutdown joining only, not
Apple event delivery or browser behavior.

`NewDesktop` never registers shortcuts, reads the clipboard, opens a browser,
requests a credential, or requests permission. An unavailable native capability
must not stop the Web UI. Configure errors should be displayed while retaining
the Web UI.

## Shortcuts

Callbacks receive a `ShortcutEvent` with `Action` equal to `open-ui`,
`translate-paste`, or `translate-ui`. Repeated-copy translation supplies an
already-validated transient `*ClipboardCapture` so the coordinator cannot read
a later, unrelated source. `CaptureError` reports a failed clipboard capture
without submitting a job. Never serialize or log the event/capture. Callbacks
run asynchronously, outside the desktop mutex, and hand off to the same
coordinator used by HTTP. Blank/whitespace disables the configurable `open-ui`
binding.

Accepted keys: A-Z, 0-9, F1-F12, Space, Return, Escape, Tab, Left, Right, Up, Down.
At least one modifier is required. Ctrl/Control, Alt/Option, Shift,
Cmd/Command (macOS), and Win/Super (Windows) are case-insensitive. Modifier order
does not matter. Duplicate normalized combinations and repeated modifiers are
rejected before registration. Letter names map to the library's native virtual
keycodes; non-US keyboard layouts are not guaranteed to match printed labels.

Additions are registered before old combinations are removed. If an addition
fails, staged registrations are unregistered and old bindings/callbacks remain.
Swapping actions reuses existing native bindings. A held shortcut fires once
until key-up. If removing an old binding fails, already removed old bindings are
re-registered, additions are discarded, and the old action map/callback remains
active. A failed configuration never activates the requested callback or action
map. Failed native unregistration does not stop the old event consumer or poison
future cleanup attempts.

Rollback itself may fail (for example, another process takes a just-released
combination). In that exceptional case the error includes `ErrHotkeysDegraded`,
`Status.hotkeysAvailable` becomes false, and **all shortcut callbacks are
suppressed**, including any native bindings that could not be removed. Further
configuration is rejected until the Desktop is closed and recreated (normally
restart the app). Web UI, credentials and text translation remain available.
The previous saved configuration must be retained on error, not replaced with
the rejected request. `Close` attempts cleanup of retained and orphaned bindings;
an OS cleanup failure is still reported. Callbacks already handed to the
application before configuration/shutdown must be cancelled by its coordinator.

Windows registration detects OS-reserved/occupied combinations. macOS event
taps do **not** provide an exclusive system-wide shortcut reservation API;
local duplicate detection and registration rollback work, but another app's
shortcut use cannot be reliably detected as a conflict. Choose non-conflicting
combinations manually. No permission dialog is requested by startup/status.

### Windows repeated-copy triggers

Windows always binds `Ctrl+C*3` to clipboard translate-and-paste and `Ctrl+C*4`
to clipboard translate-and-open-WebUI. These two bindings are product behavior,
not settings fields. The only configurable shortcut is `open-ui`; a plain
`Ctrl+C` value is rejected because it would consume the copy input needed by the
fixed gestures. macOS installs the corresponding Command+C gestures described below.

All repeat counts share one `WH_KEYBOARD_LL` hook on a dedicated locked OS
thread with a message loop. It is installed with the fixed Windows clipboard
bindings and unhooked when the desktop closes. Every hook event is forwarded
with `CallNextHookEx`. No copy input is
consumed or synthesized; the foreground application still handles every copy.
The hook filters the required key events without recording text or other keys.
There is no continuous clipboard-content monitoring or permission elevation.

The recognizer counts physical C down/up pairs while only Ctrl is held. Holding
Ctrl between taps is allowed; holding C does not count auto-repeat. Injected key
events cancel the current sequence and never contribute to it. Other key-downs
(except Ctrl), observed foreground changes, or an inter-press gap over 500 ms
reset the sequence. A 500 ms quiet interval after the last press resolves the
exact count, so four presses cannot first dispatch the three-press action. One,
two, or five-plus presses do not match. A still-held C cancels expiration.

A 25 ms message timer checks pending gestures and foreground identity without
reading clipboard contents. Before dispatching a translation action, the OS
clipboard sequence number must have changed since the last copy press;
otherwise the gesture reports a capture failure rather than submitting an older
clipboard. Slow or copy-suppressing applications may not satisfy this guard.
Sequence-number changes cannot prove which application wrote the
clipboard; clipboard managers and delayed rendering need manual acceptance.
Sensitive-content and source-capture guards still apply in the workflow.
The resolved gesture carries its foreground handle and clipboard generation to
the actual capture boundary. The foreground is rechecked before/after capture,
and the version returned by the locked clipboard read must match the gesture.
Only that bound capture is handed to the coordinator; delayed workers cannot
recapture a newly copied value. A later clipboard change still prevents paste;
target changes prevent guarded paste but not the direct-paste action below.

#### Three-copy direct paste

The fixed Windows `Ctrl+C*3` and macOS `Cmd+C*3` clipboard actions set a private
`directPaste` flag on its successful capture. `Paste` then ignores paste-only
capture failures and a missing target token. Its prewrite check and final native
dispatch validate clipboard generation, not the original target. The normal
write operation and modifier-release/cancellation checks remain. A focus,
selection, caret or editability change after capture does not cancel this path.
The source capture remains identical to other repeated-copy clipboard actions,
so recognized password/sensitive sources still fail before upload.

`directPasteDesktop` is implemented on Windows and macOS; ordinary captures and the
four-copy action cannot set this flag. It is not a user-supplied HTTP field.
Transient `direct_translating`,
`direct_pasting`, `direct_paste_skipped` and `direct_paste_dispatched` stages label
the action without promising verified original-target insertion. No focus
stealing, Enter, full-field selection or application-specific insertion is added.

New listeners remain inactive until configuration commits. A successful update
advances the configuration generation and resets partial gestures; queued events
from older generations cannot dispatch newly assigned actions. Registration and
unregistration participate in the existing rollback/degraded-state handling.
Restoring a removed listener during rollback also advances its generation, so
retired-listener callbacks cannot pass as events from the replacement.
Callbacks leave the hook thread asynchronously before entering the Desktop lock,
so listener shutdown does not wait on that lock. An observed message-loop failure
is shown in platform status and requires restart before configuring shortcuts.
Windows can remove a timed-out low-level hook without notification; no automatic
reinstallation or privileged fallback is promised. Hooks, real copy propagation,
and timing have not been desktop-accepted by automated tests.

### macOS repeated-copy triggers

macOS fixes `Command+C*3` to translate-and-paste and `Command+C*4` to
translate-and-open-WebUI. Both reuse the shared exact-count recognizer,
clipboard snapshot handoff, coordinator and diagnostic stages. Plain
Cmd+C/Command+C is reserved from the configurable open-WebUI shortcut.

A dedicated locked thread owns a synchronous CGEventTap and CFRunLoop. Every
event is returned unchanged; there is no consumed or synthesized copy input.
The synchronous tap samples the pre-copy generation before the foreground app
receives the key. Listen-only mode cannot guarantee that ordering. Callbacks
never acquire the Desktop mutex or wait for main-thread AX inspection. Window
metadata and `NSPasteboard.changeCount` are sampled only for an active copy
gesture; the listener never reads clipboard text, window titles or pixels.

Either Command key may remain held between C down/up pairs. Auto-repeat does
not count. Additional modifiers, other keys, mouse clicks and injected events
cancel a gesture; observed foreground changes and Secure Input also discard it.
A long timer gap, such as sleep/wake, discards pending presses. Reconfiguration
and queued-callback generations follow the Windows contract. A disabled tap or
permission loss marks the listener failed and suppresses callbacks until restart.
Closing signals its run loop, waits for native callbacks to finish, and only
then releases the Go handle and retained native state.

Manually enable the actual executable/host application in **System Settings >
Privacy & Security > Accessibility** and **Input Monitoring**, then restart the
application. Both are preflighted without a permission request. An executable
path/signing change may require manual reauthorization; OS policy can still
reject event-tap setup. WebUI remains usable when native shortcuts are unavailable.
No selected-text source or new settings/migration is added.

## Paste-target accessibility inspection

There is no native selected-text translation action or selection source. Native
text/range inspection exists only to verify an automatic-paste target captured
with explicitly copied clipboard text. It never submits the inspected document
or selected range for translation.

### Windows target scope

Visible, enabled Unicode native controls in the Edit/RichEdit allowlist use
bounded text and caret/selection fingerprints for target identity. The whole
control is capped at 32,767 UTF-16 units, temporary buffers are cleared, and
password/read-only/unstable targets fail closed. Unsupported control classes may
use the UI Automation target reader below; recognized native password refusals
are never retried through another reader.

### Windows UI Automation boundary

Other native control classes may expose `IUIAutomationTextPattern`. The adapter
uses the built-in CUIAutomation8 / IUIAutomation2 COM client on a locked MTA thread
per operation, requiring its connection/transaction timeouts and explicitly
disabling AutoSetFocus. There is no new runtime helper, dependency installer or
permission request. COM interfaces, BSTRs, VARIANTs and SAFEARRAYs are released
before apartment cleanup. Per-call timeout is 75 ms with a two-second inspection
budget checked between calls; the OS/provider must honor the client timeouts.
There is no unbounded abandoned worker or legacy client fallback.

The focused element must expose definite non-password/enabled/focused properties,
process identity, bounded runtime IDs and bounded ancestry to the foreground
window. Focus/password are rechecked before every text read. Exactly one native
selection/caret range and a bounded document are required. Prefix ranges cloned
from the document and moved to the selection endpoints identify the actual
position; matching text is never searched to guess a position. Document, range
text and prefix contents must agree, with valid UTF-16 and no NUL. The whole
accessible document is capped at 32,767 UTF-16 units. Two snapshots must agree.

Range failures identify which relationship was rejected: selection start before
document start, selection start after its end, or selection end after document
end. Failed comparison calls retain their relationship and HRESULT. These are
fixed diagnostic labels, not text, offsets or native identifiers. A range is
never clamped, searched for by matching text, or retried against an ancestor's
larger document. A caret-only UIA range is valid for target identity.

Only a positively editable range/value pattern yields a `uia:` paste token. The token fingerprints
foreground/process/root/element identities, document and selection/caret. It
contains digests and metadata, not surrounding text. Subsequent validation uses
the original reader before writing and
again before Ctrl+V dispatch. UIA is used for observation only: no SetValue,
SetFocus, Select, or copy-input synthesis is performed.

This provides an inspection path for eligible Cursor, VS Code, WeChat and browser
controls; it is not a blanket support claim for these applications. Application
versions, editor surfaces and accessibility settings can expose different or no
patterns. Missing/uncertain attributes fail closed. We do not change application
accessibility preferences, elevate privileges or bypass UIPI. Actual application
acceptance remains separate from mocked tests and compilation.

### macOS target scope

Existing Accessibility trust and a focused editable AX element/window in the
frontmost process are required for automatic paste. The native token retains
element, window and process identity and validates them before write and native
paste dispatch. It does not read AX selected text as a translation source. No
permission is requested automatically; application-specific AX support and Apple
compilation must be checked on a Mac, not inferred from Windows tests.

## Capture and automatic paste safety

Capture existing clipboard text on an explicit shortcut, **before** starting a
network request or opening the management browser. There is no select-all/copy
simulation. `translate-ui` can capture text even without a verified editable
target or macOS Accessibility permission. Only automatic paste requires a valid
target. Recognized sensitive clipboard marks and recognized password inputs
cause capture to fail with **no text returned**, before upload.

On Windows, a paste-only target inspection failure retains its first safe reason
and phase (initial inspection or confirmation) with that exact clipboard
capture. It does not turn safe explicitly copied text into a capture failure,
but the target token is cleared. Ordinary and repeated-copy shortcuts carry
the same reason through the translation job to `paste_skipped`; a later capture
cannot replace it or authorize paste. UI Automation diagnostics identify failing
operations/properties and HRESULTs without recording inspected text or window
identifiers. Password and sensitive/failed clipboard reads remain fatal, even
if an earlier inspection failed for a different reason. This diagnostic path
does not relax target checks, increase timeouts or retry another adapter.

Paste diagnostics also distinguish target/clipboard validation before writing,
publishing the translation, and paste dispatch after writing. In particular,
`clipboard changed` during the last phase is not evidence that the user copied
something while translation was running: it compares against the generation
returned by the write, not the initial source generation. The check still fails
closed; no dispatch-time generation is refreshed to authorize a later clipboard. Phase labels
wrap only vetted native details and preserve the underlying error identity.

Windows writes now confirm their receipt after `CloseClipboard`, not inside the
`SetClipboardData` operation: closing a write can finalize synthesized formats
and advance the sequence. One message-only owner window and OS thread span the
write/close/reopen/confirmation sequence. Before accepting the post-close
generation, the read-only locked confirmation requires that same owner and exact
Unicode output (including its terminator), with no sensitive marker and a stable
sequence. Handles are reacquired after reopening; old OS handles are not reused.
Other owners' contents are not read, even to compare identical text. Temporary
output buffers are cleared. Open/close/confirmation failure cancels dispatch,
without retrying the write or restoring an older clipboard. This applies to both
guarded paste and three-copy direct paste. Final dispatch still checks the
confirmed generation after lock release and owner-window cleanup; it never
adopts a later generation without a new explicit operation.

The caller must cancel superseded/current jobs and pass the current job's context
to `Paste`. The adapter checks cancellation before writing, after writing, and
before calling native dispatch. It waits at most two seconds for modifier
release and rechecks target/generation while waiting. The native dispatch path
checks target, generation and modifiers again. No focus stealing, activation,
Enter, selection replacement command, elevation, or timed clipboard restoration
is performed. Dispatch success is not proof the application inserted text.

After writing, the translation remains in the clipboard, including when a later
cancellation or input rejection prevents dispatch. The previous capture becomes
stale. A failure while publishing clipboard data can leave an empty clipboard;
there is no unsafe attempt to restore over a concurrent copy.

### Windows target boundary

The Win32 paste reader is restricted to enabled, visible, non-readonly Unicode native
`Edit`, `RichEdit`, `RichEdit20W`, `RichEdit50W`, `RichEdit60W`, `RichEditD2D`, and
`RichEditD2DPT` controls. ANSI windows (including legacy ANSI RichEdit), unknown
classes are not accepted by this reader; eligible unknown classes use the UI
Automation reader above. Same-process targets are refused. The foreground window, focused
control, process and GUI thread must match the captured identity.
`ES_PASSWORD` and nonzero `EM_GETPASSWORDCHAR` inputs are rejected before target
text reads and clipboard capture. Browser DOM fields,
Electron/custom-rendered editors, terminals, many Office controls and UWP controls
require the verified UI Automation contract; inaccessible/elevated controls are
not promised as supported targets;
use translate-to-UI and manually copy instead. There is no pretend universal
focus tracking or fallback to pasting into whichever window is active.

For these supported native controls, the private target token also includes a
SHA-256 digest of the control's UTF-16 text, its text length, `EM_GETSEL` selection
endpoints, and the GUI caret window/rectangle. Typing (including same-length
replacement), changed selection/caret, or changed scroll/caret geometry therefore
invalidates paste even if the same HWND still has focus. The comparison is made
before clipboard replacement, during modifier waiting, and again immediately
before native dispatch. The target's text is used **only** for this local guard:
it is never submitted for translation, logged, or persisted; temporary buffers
are cleared after hashing and only the digest/metadata is retained in the token.

`WM_GETTEXTLENGTH`, `WM_GETTEXT`, `EM_GETSEL`, and `EM_GETPASSWORDCHAR` use
`SendMessageTimeoutW` with abort-if-hung/error-on-exit flags, at most 50 ms per
message and a 400 ms overall inspection budget. Same-process and mismatched
focus-thread targets are refused to avoid same-queue timeout bypass. Only
system-marshalled messages below `WM_USER` are used; no unsafe cross-process
pointer-bearing RichEdit extensions or injected remote memory are used.
The control is capped at **32,767 UTF-16 code units**, making packed `EM_GETSEL`
endpoints unambiguous. Two text/selection snapshots must agree. Timeouts,
denied messages, inconsistent/truncated text, oversized content or inaccessible
selection disable automatic paste for that capture while clipboard-to-UI
translation remains usable. Password recognition instead fails capture outright.

There is no background focus/text watcher: the capture API has no pending-job
lifetime/context or release callback, so a watcher could otherwise retain target
access beyond a completed UI-only job. Sampling is limited to explicit capture
and paste validation. Moving focus/caret away and back, or editing and undoing
back to the exact original state between samples, is not detected. Native text,
selection, password-style and focus reads cannot form an atomic snapshot with
input delivery; last-instant changes remain an OS boundary. This fingerprint
guard is Windows-only. macOS guarded clipboard-source captures validate AX
element/window identity, not selected text/range or the entire document. Neither
platform restores a selected-text translation source.

Clipboard `CF_UNICODETEXT` reads are bounded to 4 MiB. Sensitive-format detection
includes `ExcludeClipboardContentFromMonitorProcessing`, `Clipboard Viewer
Ignore`, `application/x-keepassxc`, and `org.nspasteboard.ConcealedType`.
The Win32 clipboard is locked across generation-check and write so another
process cannot interleave its copy during that operation. `SendInput` emits
Ctrl+V with the correct 40-byte amd64/arm64 INPUT layout. Partial dispatch is an
error and releases synthetic keys without retrying paste. Windows UIPI can block
input to higher-integrity targets; its error is reported as a possible cause,
not misrepresented as an OS-confirmed diagnosis. Never elevate to work around it.

### macOS guarded-target and direct-paste boundary

Capture retains the actual AX focused element and window, plus frontmost PID.
Only AX text field/text area/combo box roles with an enabled, settable value
and accessible window qualify. AX secure-text subroles are rejected. Missing
AX attributes, inaccessible editors, noneditable elements or permission loss
result in no target/error for guarded paste.
The newest capture invalidates the previous native AX token. Events are created
with CGEvent and posted to the verified process without activating it.

Only the fixed three-copy action uses the approved direct-paste exception:
CGEvent posts Command+V to the active session rather than a verified process.
It retains initial sensitive-source refusal and rechecks permission, clipboard,
modifiers and cancellation immediately before dispatch, but intentionally does
not inspect the paste target. The four-copy action never pastes. Secure Input
prevents a repeated-copy gesture from becoming a source capture. Recognized
password/sensitive clipboard source failures remain fatal to both actions.

NSPasteboard `changeCount` is checked before/after capture and before write and
dispatch. Concealed, transient, autogenerated and KeePassXC marks are rejected.
Text capture is limited to 4 MiB of UTF-8. **NSPasteboard has no cross-process
atomic compare-and-swap or clipboard lock**: another app can still race the very
short check/clear/write interval. Focus can also change between final validation
and OS delivery on either platform, and cancellation cannot recall dispatched
input. These are genuine OS boundaries, not guarantees of atomic insertion.
Focus is sampled, not continuously monitored; moving away and back between
checks is not detected. Password detection is best-effort only for recognized
native attributes; absence of a mark is not proof that text is nonsensitive.

## Credentials and browser launch

Use different credential namespaces for development and production. Windows
uses Credential Manager via `go-keyring`. macOS calls Security.framework
`SecItemCopyMatching`, `SecItemUpdate`/`SecItemAdd`, and `SecItemDelete` directly;
secrets are never arguments to the `security` CLI or a shell, nor written to a
plaintext fallback. macOS queries disable authentication UI and iCloud
synchronization. A locked Keychain, denied ACL, or unavailable session is a
visible error requiring manual Keychain management, not a startup permission
loop. Signing/identity changes may require manual access approval. Other
processes under the same user/security context may have OS-authorized access;
the platform store is not a defense against a compromised user account.

Missing credentials return `Get == "", nil`, `Has == false, nil`, and Delete is
idempotent. A stored empty credential remains distinguishable with `Has`.
Errors are not treated as missing, so unauthenticated loopback endpoints can
work without silently ignoring credential-store failures. Namespace/account
identifiers must be nonempty UTF-8 without NUL. Store/OS size limits still apply.

`OpenBrowser` accepts absolute HTTP(S) URLs only. Windows uses `ShellExecuteW`
with a separate verb/URL; macOS uses `/usr/bin/open` with a single URL argument.
Neither interpolates a shell command. The caller is responsible for supplying
the intended local management URL. Browser access does not require a session;
the separate second-process reopen control token remains unchanged.

## Verification boundary

The initial implementation was checked on Windows amd64 with Go 1.26.1 using
gofmt, `go test ./internal/platform`, repeated mock tests, and `go vet`.
Tests inject fake bindings, clipboard generations and focus tokens and assert
normalization, duplicates, conflict rollback, action swaps, disabling, capture
privacy, context cancellation, modifier-wait clipboard changes, close behavior,
Win32 structure layout, target text/selection fingerprints, password-before-read
guards, read failures, removal rollback, and degraded-state callback suppression.
They perform **no** real clipboard access, global
shortcut registration, browser launch, credential-store mutation, permission
prompt, or synthetic input.

No macOS native compile/runtime or Windows desktop interaction acceptance has
been performed. macOS requires an actual Apple build and separately authorized
manual checks. Cross-compiled unsupported/no-cgo stubs are not native macOS
validation. A successful unit test or build must not be advertised as UI/OS
acceptance or support for every target application.
