# Desktop WebUI

The Chinese-language light interface is a desktop browser client of the local Go
server. It does not include mobile, dark-mode, PWA, or native-client variants.
Provider output and prompt snapshots are never treated as trusted HTML.
Markdown previews render through React with raw HTML disabled and remote Markdown
images replaced by text placeholders.

## Tooling

Install workspace dependencies through the root workflow. This workspace exposes
`typecheck`, `lint`, `test`, `build`, and `dev`. Production output is `web/dist`.
The parent application owns the Go embedding files in this directory.

Vite listens strictly on `127.0.0.1:19329`. Open the local Go application,
not Vite directly. HMR connects through the Go origin on port 19328, overridden
with `CVTRANSLATE_HTTP_PORT`. There is no cross-origin API access or permissive
CORS configuration.

## Behavior and boundaries

- Initial state comes from `/api/state`, without browser credentials. Exact
  Host/Origin and Fetch Metadata checks remain on the local Go API.
- Navigation follows `?page=...` and `?task=...`, including browser back/forward.
- Workspace edits submit after 600 ms idle, or immediately on blur. IME input
  waits for composition to end. Clearing/cancelling does not flush pending input.
  Language/provider/prompt changes also debounce; automatic updates replace the
  task URL instead of adding a history entry for every submission.
- Provider and prompt selectors sit in the workspace footer, beside automatic
  translation status. Their compact menus open upward when space permits, show
  the current selection, and support keyboard navigation. Prompt selection is
  shown only for prompt-capable services; selections do not save new defaults.
- The source pane uses `@mdxeditor/editor` for single-surface WYSIWYG Markdown
  editing without a formatting toolbar. Both panes default to rendered Markdown
  with only a right-aligned, two-icon source/Markdown switch beside the pane label.
  Icon buttons expose pressed state, accessible names and hover/focus tooltips. Both
  switches control the same display mode; switching from either pane commits
  pending nested source edits before changing mode, without submitting a job. The
  source's editable surface stretches through the full content area, including
  when empty, rather than occupying a small box below extra controls. Both panes
  share a minimum content height and stretch layout. Source mode is also editable;
  it is not a read-only preview. Supported rich constructs include headings, lists,
  quotes, links, code blocks and tables. HTML/JSX execution, image loading and
  uploads are not enabled. Unsupported imports (including images/raw HTML)
  retain the complete source in editable source mode with a concise notice.
- Both panes use CJK-friendly emphasis parsing, so bold/italic text ending in
  Chinese punctuation can be followed immediately by Chinese text without an
  inserted space. The editor uses matching Markdown serialization rules after
  edits. Code spans/blocks and escaped markers remain literal; clipboard-task
  loads use the default rendered mode without rewriting or resubmitting text.
- MDXEditor's initial normalization is not a user edit: task loads and mode
  switches retain the original text without submitting. Actual rich-text edits
  serialize Markdown and may normalize whitespace/list markers. External loads
  and clears reset the editor history; echoed local edits do not remount it or
  move the caret. Nested table edits are flushed before explicit submission.
  The translation pane uses a safe text-based Markdown renderer, not an MDX
  evaluator, with rendered Markdown selected by default. Display-only switches
  do not request a translation or change the text used for copying.
- Loading a history/native task never resubmits it. Editing keeps the editor
  available, discards stale results and requests cancellation of superseded jobs
  created by this workspace; it does not implicitly cancel native/history jobs.
  Cancellation is best-effort and cannot undo upstream charges. Late create
  acknowledgements are retained while editing so obsolete jobs can be cancelled
  by ID. Unchanged input does not resubmit on repeated blur; explicit retries or
  a new input/configuration edit can create another job.
- The backend reuses exact successful history before contacting the provider.
  A returned task can already be succeeded and contain `reusedFromTaskId`; the
  shared status badge shows reused and no polling is scheduled. Matching includes
  the resolved language direction and translation-affecting provider/prompt
  snapshots, not only provider IDs. Each invocation has independent local history
  and paste status; reused tasks add no token usage. There is no browser-side
  translation cache that could outlive deleted history or changed configuration.
- Only the displayed queued/running task polls, at 700 ms after each response.
  Terminal tasks, history, and inactive pages do not poll. Failed polls require
  an explicit retry. Mutations and billable calls are never retried automatically.
- API requests have finite timeouts and normally abort on component cleanup.
  Once cancellation is requested, pending create acknowledgements and cancellation
  requests finish across in-app navigation without updating the disposed workspace.
  This does not guarantee delivery when closing/reloading the browser or losing
  connectivity. A timed-out mutation may still have completed server-side;
  inspect history before retrying. Cancellation warnings remain separate from
  the current translation error and do not follow navigation to another task.
- Provider testing is a separately confirmed, potentially billed action. Preview
  uses only `/api/prompts/preview` and never calls an upstream provider.
- Template validation mirrors `internal/prompt/prompt.go`: only known tokens,
  no stray braces, 16,000 Unicode code points per template, exactly one user
  `text` token, no system `text` token, and a `target_language` token.
- Shortcut recording is draft-only until settings are saved. Registration and
  transactional rollback remain the backend's responsibility.
- Settings use translation, speech, hotkeys and local-service tabs with one
  shared draft and save action. Unmounting the speech panel stops its preview.
- Clear all history is in Settings > Translation, separate from saving settings
  and guarded by an irreversible-action confirmation. The history page retains
  single-record deletion only. Clearing history does not save or discard drafts.

## UI primitives and copy

Radix UI provides tabs, compact selects, toast notifications and confirmation
dialogs. The shared `FeedbackProvider` owns transient success/copy feedback and
asynchronous confirmations. Recoverable errors stay inline. Other form fields
keep native inputs/selects and the existing desktop light styles; there is no
additional theme or client.
Follow CONTRIBUTING.md: concise labels/status, no tutorials or slogans, and keep
necessary privacy/billing warnings and irreversible-action confirmations.
The authoritative [visual quality baseline](../CONTRIBUTING.md#webui-visual-quality-baseline)
covers hierarchy, contrast, control consistency, content overflow, interaction
states, keyboard focus and overlays; the preceding section defines typography,
spacing and card/row alignment. Apply these requirements to affected components
without introducing additional themes or clients.

The frontend uses modern desktop browser APIs, including Clipboard API,
AbortSignal.timeout, and AbortSignal.any. Clipboard writes happen only on a
user's copy click, with an explicit manual-copy fallback message.

Unit tests use fake requests/timers for translation scheduling and in-memory DOM
components for tab drafts and confirmations; they do not launch a browser or
contact providers. They also cover template boundaries and keyboard recording. Compilation and
unit tests are not browser, OS-shortcut, credential-store, or paste acceptance.
