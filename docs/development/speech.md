# Read-aloud engines and voice settings

Microsoft online TTS runs alongside the default translation adapter as a
separate `internal/speech` service injected at composition,
not a translation provider, a browser-to-Microsoft request or a platform API.
It requires no user key but is not an unauthenticated protocol.

## Saved configuration

Application Settings offers `edge` and `web-speech` engines and separate English
and Simplified Chinese voices for each. `Settings.speech` contains `engine`,
`edgeVoices: {en, "zh-CN"}` and `webSpeechVoices: {en, "zh-CN"}`. Switching the
engine preserves both sets of choices. Changes take effect on Save settings;
opening/editing/saving the form never synthesizes text or plays audio.

Edge remains the default with Andrew Multilingual/Xiaoxiao Multilingual. Ordered
migration 003 adds the speech object only to settings that lack it, preserving
translation defaults, shortcuts and all existing data. A new install receives
the same defaults. Later choices survive reopening without being reset.

The server validates the engine and both Edge voice choices against a compiled,
language-specific allowlist exposed as `speechVoices` in `/api/state`. This is a
curated catalog, not a live query or guarantee of upstream availability. Browser
voice identifiers are bounded opaque strings, never network destinations or
SSML. Invalid settings fail before shortcut changes or persistence.

The Edge POST accepts only text and language. It snapshots saved settings before
network I/O, chooses the configured voice for that language and rejects requests
when the saved engine is Web Speech. Clients cannot override the voice, engine
or endpoint per request. No engine or voice substitution is performed on error.

## Explicit draft previews

Each language's voice selector has its own Preview button for auditioning the
source/target voice using the displayed English or Simplified Chinese sample.
Only a click starts preview playback. Current draft choices apply immediately
to previews, without saving settings, reading workspace text or creating tasks.
The same control cancels loading/stops playback; another preview, engine/voice
changes, browser voice refresh, a successful save or unmount cancels the current
playback and releases its resources. Unavailable voices disable new previews
without disabling cancellation; errors remain visible and are never retried.

Edge uses `POST /api/speech/preview` with only `{language,voiceId}`. The server
requires `en` or `zh-CN`, validates the matching compiled voice ID and chooses
the fixed sample itself. It rejects caller text, SSML, endpoints and other
unknown fields. The route intentionally does not consult or modify saved
settings, so an unsaved Edge choice can be auditioned even if Web Speech is
saved. Workspace `/api/speech` still refuses per-request voice overrides and
still requires the saved Edge engine. Both routes share the same synthesis
service, concurrency slots, timeout, cancellation, origin checks and audio
response validation. No persistence migration is needed.

Browser previews use the draft browser voice URI and the displayed sample
through the existing Web Speech controller without calling either Edge route.
Samples in the frontend and server must stay aligned. Browser/OS voices may be
online; Edge previews send only the fixed sample to Microsoft. Neither engine
starts playback merely because the form opens, changes or saves.

## Web Speech API

The frontend uses `window.speechSynthesis` and `SpeechSynthesisUtterance` only
when `web-speech` is selected. It never calls `/api/speech` in that mode. Actual
voice availability and synthesis limits depend on the current browser and OS;
some voices use a remote service, so this option is not necessarily offline.

Voices come from `getVoices()` and asynchronous `voiceschanged` notifications.
The settings form separates English locales from Simplified Chinese locales,
offers a manual refresh and reports empty/unsupported lists. Explicit choices
are stored by `voiceURI`; if absent in the current browser they remain visible
as unavailable rather than being silently reset. An empty identifier explicitly
selects the matching-language browser default, or a matching available voice.
No matching voice or a missing saved voice produces an actionable error.

Both engines use the same text/language selection and stop controls. Each
workspace reading captures the text and language at the explicit click and owns
its synthesis/playback lifecycle independently of translation. Submission,
polling, completion, task URL changes within the workspace, and text/language
edits or clearing do not cancel it. The active control remains available to
cancel/stop even if its pane becomes empty or exceeds the input limit.

Explicit stop, starting the other pane's reading, changed speech configuration
or unmount releases the current reading. The hook compares configuration values,
not settings object identity, so an equivalent settings refresh does not dispose
playback. Browser callbacks are generation-guarded and detached on cancellation
or disposal. Browser speech does not automatically fall back to Edge, nor does
Edge fall back to browser speech. Draft preview cancellation remains as described
above.

## Protocol

The current Read Frog reference uses HTTPS, not the older Edge Read Aloud
WebSocket transport. CVTranslate independently implements the HTTP protocol:

1. POST an empty body to
   `https://dev.microsofttranslator.com/apps/endpoint?api-version=1.0` with
   the Microsoft consumer client's published identification and `X-MT-Signature`.
   The signature is HMAC-SHA256 over lowercased client ID, URL-encoded schemeless
   endpoint, GMT date and random nonce, formatted with the client ID/date/nonce.
   This fixed protocol material is not a user secret or Azure subscription key.
2. Validate the bounded JSON response's `t` token and `r` region. The token is
   printable header text; region must be a single lowercase alphanumeric label.
   Construct only `https://<region>.tts.speech.microsoft.com/cognitiveservices/v1`.
   Neither the browser nor the bootstrap response can supply an arbitrary URL.
3. POST XML-escaped SSML with the token exactly as returned in `Authorization`.
   Use `X-Microsoft-OutputFormat: audio-24khz-48kbitrate-mono-mp3` and require a
   successful `audio/mpeg` response. Reject incomplete/empty/oversized bodies.
4. Return only MP3 bytes to the browser. Obtain one token for each user-requested
   synthesis and reuse it only for that request's chunks, then discard it. No
   token cache, disk audio cache, user credential writes or token refresh retry.

Default voices are `en-US-AndrewMultilingualNeural` and
`zh-CN-XiaoxiaoMultilingualNeural`; the configured allowlisted voice replaces the
default after saving. Explicit `en`/`zh-CN` chooses that language's voice. `auto`
selects Chinese when Han characters occur, otherwise English; it is deliberately
a two-language heuristic rather than a language-detection API.

## Limits and lifecycle

- Input: valid UTF-8, non-whitespace, at most 20,000 Unicode code points; reject
  unsupported XML control characters instead of treating input as markup.
- Chunk at UTF-8 boundaries, preferably whitespace, at most 1,800 raw bytes per
  request. This is a conservative client policy, not a documented provider quota.
- Concatenate sequential MP3 output only after every chunk succeeds. Any failed
  chunk discards the partial result; never silently play an incomplete reading.
- Whole synthesis timeout: 120 seconds, including bootstrap and all chunks.
  Browser request timeout: 130 seconds. Explicit stop, another reading, changed
  speech configuration or leaving the workspace aborts the request and upstream
  context; translation updates and edits do not. Application shutdown also
  cancels HTTP request contexts.
- At most two active synthesis operations for the process, with immediate busy
  errors instead of an unbounded queue. Audio response cap: 16 MiB; bootstrap
  cap: 32 KiB. No automatic retries, redirects, provider or offline fallback.
- Playback uses temporary object URLs. Stop, failure, completion and unmount
  release audio resources and URLs; generation guards reject late results/events.

Only explicit Edge read-aloud or preview clicks send text to Microsoft; startup, save,
translation completion and voice selection do not. The UI discloses each
engine's different network/privacy behavior. Audio,
tokens, source text and upstream error payloads must not enter diagnostics.
The local POST endpoint requires exact local Origin and retains Host/Fetch
Metadata validation. No browser session or CSRF token is required; see
the [architecture security section](../architecture.md#security).
`media-src 'self' blob:` permits local audio; external `connect-src` is not added.

## Verification and reference

Tests use local HTTP mocks, not real Microsoft traffic. Cover signing, routing,
SSML escaping, response bounds, cancellation, partial results, redirects, safe
errors, local-origin checks, playback lifecycle and URL cleanup. Actual service
availability and browser/OS audio playback require separately authorized checks.
These consumer endpoints have no guaranteed quota, compatibility or continued
availability; upstream service terms still apply.

Protocol reference: Read Frog revision
[`1308e8d1be455d08ec4e907eb2c8966998abca0b`](https://github.com/mengxi-ream/read-frog/tree/1308e8d1be455d08ec4e907eb2c8966998abca0b/src/utils/server/edge-tts)
(`signature.ts`, `endpoint.ts`, `synthesize.ts`). Its GPL source was inspected
but not imported, copied or mechanically translated. CVTranslate uses original
Go HTTP/crypto/XML code; there is no new runtime dependency.
