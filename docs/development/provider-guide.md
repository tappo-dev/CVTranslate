# Adding a provider

1. Document protocol, authorization, supported languages, prompt capability,
   endpoint restrictions, limits and billing behavior.
2. Implement `provider.Adapter` in `internal/provider`: `Capabilities`,
   `ValidateConfig` and `Translate`. Add its type to `compiledAdapters()` in
   `registry.go`. `Registry()` exposes `defaultModel`, `supportsPrompts`,
   `supportsApiKey`, `configurableEndpoint` and `requiresModel` metadata to every
   caller. Advertise actual capabilities; unsupported fields must not become
   mandatory in the UI.
3. Validate its configuration. Keys are injected by the credential service;
   never access storage, OS APIs, global configuration or browser state here.
4. Map language identifiers and upstream errors into the shared contract.
5. Respect context cancellation and timeouts. Bound response bodies. Never
   automatically retry billable calls or follow credential-carrying redirects.
6. Add necessary configuration UI only; disable unsupported prompt controls.
7. Test with an in-process mock HTTP server: success, malformed/empty/truncated
   output, authorization, rate limit, timeout/cancellation, language mapping and
   any optional usage metadata.
8. Update supported-provider documentation and run `npm run check`.

Normally this does not modify shortcut or job lifecycle implementations. A shared
contract change requires an impact review and updates to every direct caller.

`provider.TranslationRequest` carries raw `Text`, `SourceLanguage` and
`TargetLanguage` separately from optional `SystemPrompt` and `UserPrompt`.
Adapters return `TranslationResult` with translated `Text` and optional
`TokenUsage`. Report usage only when the upstream protocol supplies validated
counts; never estimate it locally or convert it to currency. Missing usage must
not turn a valid translation into an error.
Pure translation adapters consume the raw fields and advertise
`SupportsPrompts: false`; they must not parse LLM prompts to recover input. The
translation service renders prompts only for prompt-capable adapters. Extend
provider-specific configuration fields/forms only when the new protocol needs
them; a registry entry alone cannot make a different vendor's settings valid.
Optional template personas are composed by `prompt.Render` into `SystemPrompt`,
not interpreted by adapters or sent in a vendor-specific field. They add no
second request. Raw `Text` remains unchanged and pure translation adapters never
receive persona instructions. Preview and task execution share the same renderer.
The saved automatic target mode is resolved by the translation service before
adapter dispatch, so providers receive only concrete `en` or `zh-CN` targets.

The built-in system template asks prompt-capable providers to preserve code
verbatim, including unfenced snippets and natural-language text within code,
and translate only surrounding prose. The rule takes priority over persona and
target-language guidance, but is not enforced by adapters or a local code parser.
Custom templates remain user-controlled; pure translation adapters such as
Microsoft receive no such instructions and retain their existing behavior.

## OpenAI-compatible adapter

`openai-compatible` uses POST `<baseUrl>/chat/completions`, with `model`,
`messages` and `stream: false`. New configurations default to `gpt-6-luna` from
compiled capability metadata; model names remain user editable and existing
saved values are not migrated. It requires a
complete nonempty assistant text response with normal completion status. Refusal,
tool calls, content filters, truncated output and malformed bodies are errors.
It does not promise Responses API or every vendor-specific optional parameter.
Successful responses may include standard `usage.prompt_tokens`,
`usage.completion_tokens` and `usage.total_tokens`. Nonnegative, internally
consistent values are returned to the translation service and persisted with a
successful task. Missing, malformed or unreasonable usage is ignored while the
translation result remains valid.

Base URLs accept HTTP and HTTPS, including remote HTTP endpoints, without an
opt-in flag. The editor warns about plaintext text/API key transmission over
HTTP but does not block saving. URLs must not contain credentials, query
parameters or fragments. Endpoint changes still require confirmation to reuse
an existing key, including changes from HTTPS to HTTP on the same host.

Keep secrets and upstream payloads out of error messages. Use stable error codes
and safe actionable descriptions. A successful test connection can incur billing
and must be explicitly requested, never run during ordinary save or startup.

## Built-in Microsoft Translator

`microsoft-translate` uses POST
`https://edge.microsoft.com/translate/translatetext` with query parameters
`from`, `to` and `isEnterpriseClient=false`. The body is a JSON string array,
not the older `[{Text: ...}]` form. No API key or bearer token is sent. Language
mapping is `auto` -> empty source, `en` -> `en`, `zh-CN` -> `zh-Hans`.

The configuration has empty `baseUrl`/`model`; the endpoint is compiled in.
It uses raw translation text, not prompts; `defaultModel` is empty and the four
boolean optional capabilities above are false. Input is UTF-8 and bounded to 20,000 code points, response to
2 MiB. Text is HTML-escaped before submission because the service aligns tags;
returned entities are decoded exactly once and rendered as text, never HTML.
Missing/empty, malformed, incomplete or wrong-language responses fail safely.
There are no redirects, automatic retries or fallback providers.

Migration 002 seeds `builtin-microsoft` once with a 30-second timeout, selecting
it for new installs and existing empty defaults only. Later user edits/deletion
are preserved on restart. Microsoft paths do not read or write user credentials;
OpenAI paths still fail visibly when the credential service is unavailable.
Existing provider types are immutable: create/copy a new instance to change type
rather than accidentally transferring a saved key to a different protocol.

The consumer endpoint currently needs no user account/billing configuration but
has no contracted quota or availability guarantee. Explicit connection testing
still sends text to Microsoft. No real service calls belong in automated checks.
Read Frog's [protocol implementation](https://github.com/mengxi-ream/read-frog/blob/1308e8d1be455d08ec4e907eb2c8966998abca0b/src/utils/host/translate/api/microsoft.ts)
was inspected as a reference; its GPL source was not copied.
