# Architecture

## Scope

Windows/macOS per-user background process with a browser management UI. English
and Chinese initially. No Android, mobile Web, PWA, browser extension or native
main window. Read Frog is an interaction/protocol reference, not imported source.

The desktop WebUI supports light and dark themes through shared semantic CSS
tokens. Light remains the default. The topbar toggle beside GitHub stores only
`cvtranslate.theme` in localStorage for the current browser origin; it does not
change backend settings, drafts, translation state or native appearance. A blocked
storage write leaves the current page theme usable and reports that it could not
be remembered. No database migration or system-theme permission is involved.

## Dependency boundaries

`domain` contains shared data contracts. `provider` implements translation protocols
and depends only on domain/standard networking. `prompt` validates and renders
templates. `storage` persists domain values in SQLite. `translation` orchestrates
jobs through injected store, provider and credential interfaces. `platform` owns
OS integration. `server` owns origin-checked loopback HTTP; `cmd/cvtranslate` composes them.
Neither provider nor domain may import storage, platform or server.

`speech` implements on-demand Microsoft HTTPS speech authorization and synthesis
without importing storage, platform or server. The composition root injects it
into the local server. Audio/text/token data remain in request memory;
speech does not create translation jobs or alter translation defaults.
Persisted speech settings select either that Edge service or browser Web Speech,
with independent English/Chinese voice choices for both engines. The frontend
uses only the saved engine for workspace text; settings previews explicitly use
the current draft engine/voice with fixed samples and never save the draft.
There is no silent fallback. Edge voices come from
a compiled language-specific allowlist, browser voices from the current browser.

All inputs use the same job pipeline. Provider configuration and prompt contents
are snapshotted before execution. Task states are queued, running, succeeded,
failed, cancelled and interrupted. Paste outcome is separate from translation.
Shutdown/restart never automatically resends jobs or pastes old results.

Before credential access or provider dispatch, the service checks successful
history for the exact source text, resolved language direction, provider instance,
type, endpoint and model, plus the prompt identity, templates and persona when
applicable. Names, timeouts and prompt revision metadata do not affect the match.
Microsoft ignores unused prompts. A hit creates an immediately succeeded local
task with `reusedFromTaskId`, the current snapshots and an independent paste
outcome; it carries no new token usage and never calls the provider. Native
shortcuts still require a new explicit gesture and retain their existing capture
and paste guards. History lookup/save failures reject the request rather than
silently issuing another possibly billable call. Only successful retained results
are reused, not active/failed/cancelled/interrupted tasks. Clearing history removes
reuse data; there is no additional durable cache. Migration 006 adds only a lookup
index, without rewriting settings, snapshots or user text.

## Provider and language contracts

Use the provider guide. Provider types are compiled into a registry; user-defined
instances contain endpoint/model configuration. Languages use stable identifiers
(`auto`, `en`, `zh-CN`) rather than provider-specific labels. Sources use `auto`
for provider detection. Targets may use `auto` for the application's bounded
English/Chinese direction selection: text containing a Han character becomes
Chinese-to-English, otherwise it becomes English-to-Chinese. The translation
service resolves that mode to concrete source and target languages before prompt
rendering, provider dispatch and task persistence. Capability metadata
distinguishes prompts, API keys, configurable endpoints, required models and the
code-defined default model for newly created configurations. OpenAI-compatible
providers default new records to `gpt-6-luna` without rewriting existing data. New
installs default to the built-in Microsoft Translator with automatic source and
target selection. Migration 002 seeds it and fills only an empty existing default,
without replacing user-selected providers, history or prompts. Microsoft has a
fixed endpoint and never consults the keyring; OpenAI credential rules remain.

## Security

Listen on 127.0.0.1 only. Browsers open the local address directly without
session cookies, bootstrap tickets or CSRF tokens. Exact Host validation,
rejection of foreign/opaque Origin headers, mandatory exact Origin on mutations,
and Fetch Metadata rejection of cross-site/same-site cross-origin requests
remain; no wildcard CORS. These checks do not authenticate native clients:
other local users/processes able to reach the port can access data and invoke
management operations. Instance and development-helper control tokens remain
separate. Do not expose or forward the management port to other machines.
Never log secrets or text payloads.
Credentials live in Windows Credential Manager/macOS Keychain, not SQLite or API
responses. Credential failure is visible; no plaintext fallback. Endpoint host
or scheme changes cannot silently reuse a key. Provider endpoints accept HTTP
and HTTPS, including remote HTTP test services. The UI warns that HTTP transmits
text and any API key unencrypted, but does not block saving or require an opt-in.
Endpoint URLs cannot contain credentials, query parameters or fragments; no
credential-carrying redirect following.

Prompt rendering is one-pass substitution, not executable templates. Browser
output is untrusted text, not HTML. No telemetry or cloud synchronization.
`update` performs a user-initiated GitHub latest-release query for the fixed
upstream repository and compares Semantic Versions. It never runs automatically,
sends no identifiers or local data and imports no storage, platform or server
code; the composition root injects it into the server.
History is sensitive local plaintext; filesystem permissions are not encryption.

Templates may include an optional plain-text persona (up to 4,000 code points).
The prompt layer appends it after system-template rendering, together with
meaning-preservation and target-language guidance. Persona text is never parsed
for template variables. An empty persona leaves rendering unchanged. This applies
only to prompt-capable providers and uses the same service for WebUI and native
shortcuts. Persona instructions travel to the selected model with the prompt and
are included in the existing immutable task snapshot. These are model instructions,
not a guarantee of semantic fidelity. Migration 004 adds empty personas only to
templates missing the field; it leaves defaults, revisions and history untouched.
Migration 005 updates only the read-only `builtin-general` template to revision 2
with the Serious Developer persona. Custom templates and historical prompt
snapshots remain unchanged.

Migration 007 updates only the read-only `builtin-general` system template to
revision 3. It instructs models to preserve recognizable fenced, indented, inline
and unfenced code verbatim, including comments, strings and HTML/XML tag text,
and translate only surrounding prose. Code preservation takes priority over
target-language and persona instructions. This remains prompt guidance, not
local code detection or output enforcement, and does not apply to Microsoft.
Custom templates, defaults, personas and historical snapshots are not rewritten.
The changed system template prevents reuse of results from the older built-in
prompt without deleting that history.

Provider adapters return translated text plus optional token usage. The
OpenAI-compatible adapter accepts only valid standard `usage.prompt_tokens`,
`usage.completion_tokens` and `usage.total_tokens` values; missing or invalid
usage never invalidates an otherwise successful translation and is not estimated.
Successful task history stores the optional usage snapshot. Storage aggregates
those snapshots by caller-supplied local timezone for the token report; failed
tasks and providers without usage are excluded. Deleting history also removes
its contribution because no separate accounting copy is maintained.

Read aloud is explicit and independent of the translation provider. Web Speech
does not call the local synthesis endpoint, though browser/OS voices may use
their own online services. The Edge POST endpoint retains exact Host/Origin
and Fetch Metadata checks; browser connections remain
same-origin. Only `media-src` adds `blob:` for in-memory MP3 playback. Temporary
Microsoft tokens never reach the browser or database. Synthesis hosts are derived
from validated region labels under Microsoft's fixed speech domain, never an
arbitrary returned URL. See the [speech protocol](development/speech.md).

Migration 003 adds only missing speech settings, retaining the existing Edge
defaults on upgrade. Engine/voice settings are validated before saving; Edge
requests snapshot the saved configuration, reject the Web Speech engine and
never accept arbitrary per-request voice names, SSML or endpoints.
The separate preview route accepts only an explicit language and matching
catalog voice ID, synthesizing a server-owned fixed sample without consulting
or changing saved settings. It shares the same origin checks, bounded synthesis
service and in-memory audio lifecycle; it cannot override workspace reading.

## Desktop and development

Windows exposes a native notification-area icon, and macOS a Cocoa menu-bar icon,
with open-WebUI and quit actions.
The platform adapter only queues events; the composition root owns browser work
and graceful shutdown. Quit cancels shortcut/paste work and translations, removes
native registrations and drains HTTP before closing storage. A lost tray stops
the application rather than leaving an invisible process without an exit entry.
Explorer restart re-registers the Windows icon. macOS creates/removes an
`NSStatusItem` on the existing Cocoa main loop; menu targets only set atomic
actions, consumed by a Go worker without main-thread dispatch. The menu requires
no shortcut permissions, and Quit never invokes Cocoa's immediate termination
path. `Close` joins the event worker before removing native resources. No browser
shutdown endpoint, persistence change, new runtime dependency or native main window is
introduced. `--no-native` and `--version` do not create an icon.

The sidebar and favicon share `web/public/icon.svg`. Native adapters embed PNGs
generated from that same source; macOS uses a monochrome template tinted by
AppKit. Rasterization is development-only, not a runtime dependency. The shared
checks reject stale assets; see [icon maintenance](development/icons.md).

Translation shortcuts use explicitly copied clipboard text only. On Windows,
`Ctrl+C*3` is fixed to translate-and-paste and `Ctrl+C*4` is fixed to submit the
shared translation job and open its local result page using `?task=ID`. macOS
uses fixed `Cmd+C*3` and `Cmd+C*4` gestures for the same actions. These
clipboard gestures are shown as static settings information, not editable
bindings. Open-UI-only remains a separate configurable shortcut. Native
selected-text translation and its task entries are not part of the application
contract.

Windows paste-target verification supports Unicode Edit/RichEdit controls plus
eligible UI Automation text controls. UI Automation requires focused,
non-password, stable text/range and target identity; it is not unconditional
support for all editors. macOS uses focused editable AX target identity for
guarded paste, not the three-copy direct-paste action. Guarded automatic paste
requires an unchanged identifiable target and clipboard, released modifiers
and a current job. No forced focus, select-all/copy, Enter or privilege elevation.
Uncertain targets fall back to manual copying. Translation replaces the clipboard
on paste; no timed restoration. Input dispatch is not proof of insertion.

The Windows fixed gestures share a pass-through keyboard hook that counts the
gesture without recording input or polling clipboard contents. Exact counts are resolved after
a 500 ms quiet interval with a maximum 500 ms inter-press gap; holds/auto-repeat
do not increment the count. Clipboard translation requires an observed generation
change after the last copy press. macOS uses the same recognizer contract with a
pass-through CGEventTap on a dedicated run loop. The callback returns the original
event unchanged after sampling its pre-copy clipboard generation; a listen-only
tap could observe the event after the application has already copied. No input
text is recorded, and clipboard contents are not continuously polled.
Accessibility and Input Monitoring are preflighted without requesting permission; manual grants and an
application restart are required. Windows UI Automation target inspection and
transient shortcut diagnostics are described in [native integration](development/native.md).

The Windows fixed `Ctrl+C*3` and macOS fixed `Cmd+C*3` actions send Ctrl+V or
Command+V respectively to the then-active window after
translation without paste-target, editability, caret or selection verification. Initial sensitive
source capture, cancellation, clipboard generation and modifier checks remain.
Settings/status label this unverified behavior; the four-copy actions never paste.
It can paste into an unintended window. This direct-paste exception does not
weaken guarded paste or restore selected-text capture. No settings fields,
migrations or HTTP paste operations are added. The macOS native listener and
dispatch have not been compiled or tested on the Windows implementation host;
macOS build and native acceptance remain required.

Development data, credential service name, locks and ports are separate from
production. Development proxies Vite through the same Go origin and retains
the same Host/Origin/Fetch Metadata checks. Release embeds assets and excludes
the development proxy.
