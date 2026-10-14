# Local API contract

All JSON uses camelCase. Errors are `{ "error": { "code": "...", "message": "safe text" } }`.
The API is local-only and requires no browser credentials. Validate the exact
Host and any supplied Origin; mutations require the exact local Origin. Reject
cross-site and same-site cross-origin Fetch Metadata. No wildcard CORS, session
cookies, CSRF tokens or credentials in localStorage. These are browser-origin
checks, not protection against native clients on the same machine.

- Browser opens `/` or `/?task=ID` and directly fetches `/api/state` without
  credentials. `/api/session` has been removed and returns 404, not a bootstrap
  response. Browser-open URLs contain no authorization material or expiry.
- GET `/api/state`: `{providers: ProviderConfig[], providerTypes: {type,name,defaultModel,supportsPrompts,supportsApiKey,configurableEndpoint,requiresModel}[], prompts: Prompt[], settings:
  Settings, languages: Language[], speechVoices: {id,name,language}[], platform: {os, hotkeysAvailable, pasteAvailable,
  message, shortcut?: {action, stage, message, occurredAt, taskId?}}, version:
  string, development: boolean}`. Domain types match domain/models.go.
  `shortcut` is transient diagnostic state (absent with native integration
  disabled); an empty action means no received shortcut. It includes neither
  source/result text nor native tokens. Settings refreshes this status without
  replacing its unsaved configuration draft. Task links can be opened directly
  in any local browser while the service is running.
- POST `/api/providers`: ProviderConfig (omit/empty id for create) plus optional
  `apiKey`, `clearApiKey`, `confirmEndpointChange`. Returns ProviderConfig.
  Never return the stored key. Cloning a config does not copy credentials.
  `baseUrl` accepts HTTP or HTTPS, including non-loopback HTTP, with no opt-in
  field. URL credentials, query parameters and fragments are rejected. The UI
  warns about plaintext transmission over HTTP without blocking saving. Reusing
  a stored key after changing the endpoint still needs `confirmEndpointChange`,
  including HTTPS-to-HTTP changes on the same host.
  These endpoint/model/key fields apply according to the provider capabilities.
  A newly created provider whose type requires a model uses that type's
  `defaultModel` when the request leaves `model` empty. For
  `openai-compatible` this is the code-defined `gpt-6-luna`; existing records
  are never rewritten by this default.
  `microsoft-translate` has a fixed compiled endpoint, empty `baseUrl`/`model` and
  no API key or prompts. Its keyring operations are skipped, not downgraded to
  plaintext credentials. Existing provider types cannot be changed in place
  (`provider_type_immutable`); create/copy a new instance instead.
- DELETE `/api/providers/{id}`: 204. Default providers cannot be deleted/disabled.
- POST `/api/providers/{id}/test`: `{}`; returns `{ok:true}`; explicit upstream
  request that may be billed for OpenAI-compatible providers. Microsoft tests
  require no user key but still transmit the test text.
- POST `/api/prompts`: Prompt (empty id for create); returns Prompt. Built-ins are
  immutable. Server assigns revision. Default prompts cannot be deleted.
  `persona` is optional plain-text style guidance, at most 4,000 Unicode code
  points; empty or whitespace-only disables it. Saved edits use the same revision
  check as template changes. Personas are not executable templates and do not
  expand variables. Invalid or oversized personas return 400 `invalid_prompt`.
- DELETE `/api/prompts/{id}`: 204.
- POST `/api/prompts/preview`: `{systemTemplate,userTemplate,text,sourceLanguage,
  targetLanguage,persona?}`; returns `{system,user}`. No upstream request.
  Uses the same persona composition as translation tasks; the target must be
  concrete (`en` or `zh-CN`). The editor selects a concrete preview target even
  when the saved translation default is automatic.
- PUT `/api/settings`: Settings; returns Settings. Port changes apply at restart.
  Hotkey updates apply transactionally; errors preserve original bindings/settings.
  `speech` is `{engine, edgeVoices: {en,"zh-CN"}, webSpeechVoices: {en,"zh-CN"}}`.
  Engines are `edge` and `web-speech`. Edge voice IDs must match the corresponding
  language in `/api/state.speechVoices`, a built-in catalog fetched without any
  upstream request. Web Speech IDs are opaque browser `voiceURI` values (empty
  means language-matching browser default), at most 1,024 UTF-8 bytes and without
  control characters. Their actual availability is checked in the browser, not
  the server. Both engines' choices are retained when switching. Invalid speech
  settings return 400 `invalid_speech_settings` without changing any settings or
  hotkeys. Saving never synthesizes audio. Migration 003 adds missing settings
  with Edge and the existing default voices without replacing other data.
- POST `/api/tasks`: TranslationInput; returns Task with HTTP 202. Browser submits
  entry `web`; server reserves `clipboard-ui`/`clipboard-paste` for native
  actions. Removed `selection-ui`/`selection-paste` entries are rejected. Browser
  input cannot choose a native source or trigger native capture/paste through
  this endpoint. A target
  language of `auto` is resolved before dispatch: a source of `auto` is first
  classified as Chinese when the text contains a Han character and English
  otherwise, then the opposite supported language is selected as the target.
  Tasks persist the resolved concrete direction.
  Before dispatch, successful history is matched by exact source, resolved
  languages, provider instance/type/endpoint/model and applicable prompt
  identity/templates/persona. A match returns a new task already `succeeded`,
  with `reusedFromTaskId` and no `tokenUsage`; it does not call a provider or read
  credentials. Its entry, snapshots and paste outcome belong to this invocation.
  HTTP 202 is unchanged; callers must inspect status instead of assuming a queued
  task. A history lookup/save error rejects the request without an upstream
  fallback. There is no separate cache surviving deletion of all matching history.
- GET `/api/tasks/{id}`: Task. Poll at ~700ms while queued/running.
- Successful OpenAI-compatible tasks may include
  `tokenUsage: {promptTokens,completionTokens,totalTokens}` when the upstream
  response reports valid standard usage. Missing or invalid usage is omitted,
  not estimated. Failed and cancelled tasks do not retain usage.
- POST `/api/tasks/{id}/cancel`: `{}`; returns Task.
- POST `/api/speech`: `{text,language}` where language is `auto`, `en` or `zh-CN`.
  Returns raw `audio/mpeg` bytes with `Cache-Control: no-store`, not JSON/base64
  or a remotely accessible URL. Errors use the normal safe JSON envelope.
  Requires exact local Origin like other POST endpoints. No caller
  endpoint, key, voice, engine or SSML is accepted. The server snapshots saved
  speech settings and uses the selected Edge voice for the resolved language.
  If the saved engine is Web Speech it returns 409 `speech_engine_mismatch`
  without contacting Microsoft. Browser speech runs in the browser instead and
  never invokes this endpoint. Text is 1-20,000 code points, audio
  at most 16 MiB; two concurrent requests per process, 120-second total timeout.
  `auto` uses Han presence for Chinese, otherwise English. The request's context
  cancels upstream work. Synthesis is independent of translation jobs and never
  stores audio/tokens/history or retries automatically. Errors include 400
  `invalid_input`/`invalid_language`, 429 `speech_busy`/`rate_limited`, 504
  `timeout`, 408 `cancelled`, and 502 for upstream/format failures. See
  [speech](speech.md) for the protocol and privacy boundary.
- POST `/api/speech/preview`: `{language,voiceId}` with explicit `en` or `zh-CN`
  and a matching voice ID from the built-in Edge catalog. Returns uncached MP3
  using the same synthesis service, limits, cancellation and safe errors as
  `/api/speech`. The server chooses a fixed language sample; no caller text,
  SSML, endpoint or engine is accepted. Invalid language/voice pairs return 400
  `invalid_speech_preview`; unknown JSON fields return 400 `invalid_json`.
  This explicit draft-only preview works regardless of the saved engine and
  never saves configuration or creates translation tasks. It does not change
  `/api/speech`'s saved-engine/voice contract. Exact Host/Origin and Fetch
  Metadata checks are unchanged. Browser voice previews do not call this API.
- POST `/api/update-check`: no body; requires the exact local Origin like other
  POST endpoints. Called only when the user clicks Check for updates; never on
  startup or page load. The server sends one unauthenticated GET to GitHub's
  `repos/tappo-dev/CVTranslate/releases/latest` (10-second timeout, no retry, no
  redirects, no identifiers or local data). Returns
  `{currentVersion, latestVersion, status, releaseUrl}` where `status` is
  `update_available`, `up_to_date`, `no_release` (GitHub 404; `latestVersion`
  empty) or `not_comparable` (running build such as `dev` is not SemVer).
  Versions are compared as Semantic Versioning 2.0.0, ignoring a `v` prefix and
  build metadata. `releaseUrl` is built locally from the fixed repository URL and
  the validated tag, never taken from the response. Errors: 502
  `network_failed`/`upstream_failed`/`invalid_response`/`update_check_failed`,
  429 `rate_limited`, 504 `timeout`, 408 `cancelled`, 503
  `update_check_unavailable`. The WebUI keeps the repository link usable when
  the check fails.
- GET `/api/history?page=1&pageSize=20&q=...&status=...&createdAfter=...&createdBefore=...`:
  TaskPage. The optional creation bounds are RFC 3339 timestamps; `createdAfter`
  is inclusive and `createdBefore` is exclusive. The WebUI converts selected
  local calendar dates to these bounds before requesting history.
- GET `/api/token-report?days=365&timezoneOffsetMinutes=480`: `TokenReport`
  with `today`, `last7Days`, `last30Days` summaries and a zero-filled `daily`
  array. Each summary contains `requestCount`, `promptTokens`,
  `completionTokens` and `totalTokens`. `days` defaults to 365 and must be
  30-366. `timezoneOffsetMinutes` is minutes east of UTC and must be within
  -840 through 840; the WebUI sends the browser's current offset. Only
  succeeded tasks with recorded usage are counted. The endpoint performs no
  provider request and does not convert tokens to currency.
- DELETE `/api/history/{id}`: 204, active jobs cannot be deleted.
- DELETE `/api/history`: 204, fails if active jobs exist; UI must confirm.

The separate `POST /internal/open` process-control endpoint still requires its
`X-Instance-Token`. It opens the local page without returning the control token.
Provider keys and this process-control token are not browser credentials and
are never exposed to the browser.

Application navigation uses
`?page=translate|history|tokens|providers|prompts|settings`;
task links use `?task=ID`. History date links use `?page=history&from=YYYY-MM-DD&to=YYYY-MM-DD`.
The Token heatmap opens a single-day history link on double-click. Language IDs
are `auto`, `en` and `zh-CN`. `auto` is
valid for sources and translation targets; automatic targets are resolved by the
translation service and are never forwarded to providers or prompt rendering.
Prompt tokens are source_language, target_language, text inside double braces.
For prompt-capable providers, a nonempty persona is appended to the rendered
system template with guidance to preserve meaning, facts and important details
while adapting tone naturally in the selected target language. User text is not
rewritten locally. Pure translation providers ignore templates and personas.
`Task.promptSnapshot.persona` records the submitted template's persona; later
edits do not change running jobs or history. Migration 004 adds only missing
persona fields to saved templates as empty strings. It does not change template
revisions, defaults or historical JSON; old snapshots decode to an empty persona.
Migration 005 updates only `builtin-general` to revision 2 with the Serious
Developer persona; custom templates and historical snapshots are not rewritten.
Settings persist only the configurable `openUi` shortcut. Strings such as
`Ctrl+Space`, `Ctrl+Alt+T` and `Cmd+Shift+T` are accepted; empty disables it.
Windows/macOS clipboard translation is fixed behavior rather than configuration:
Windows `Ctrl+C*3` and macOS `Cmd+C*3` submit `clipboard-paste`; the corresponding
four-copy gestures submit `clipboard-ui` and open that task without pasting.
Three copies use direct, unverified Ctrl+V/Command+V to the then-active window,
not an original-target verification. Sensitive-source refusal, clipboard
generation, cancellation and modifier guards remain. Plain Windows `Ctrl+C`
and macOS `Cmd+C` (including the `Command` alias) cannot be used for `openUi`.
Repeated bindings remain invalid custom settings. The maximum inter-press gap
and final quiet interval are both 500 ms; holds/auto-repeat do not count.
macOS requires manually granted Accessibility and Input Monitoring permissions
and restart; its pass-through CGEventTap preflight never requests permission.
Availability/errors use the existing platform status contract, with no new
settings fields, database migration or HTTP trigger/paste endpoint.
Legacy `translatePaste`, `translateUi`, `selectionPaste` and
`selectionUi` values are ignored when old settings JSON is read and are removed
on the next settings save; no database reset or destructive migration is used.
Translation tasks retain snapshots, not secrets. Status: queued/running/succeeded/
failed/cancelled/interrupted. Paste status is independent and does not prove insertion.
