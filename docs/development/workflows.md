# Development workflows

## Prerequisites and scope

Windows/macOS desktop development only. Use Node >=22.12 with npm, the Go version
declared in `go.mod`, and the repository lockfiles. Install project dependencies
explicitly with `npm ci` and `go mod download`; none of the tooling installs SDKs, Delve, editor
extensions or OS permissions. On macOS, native CGO integration requires Apple's
command-line compiler tools. F5 additionally needs the Go editor extension and
Delve on PATH. `npm run doctor -- --debug` diagnoses those prerequisites without
installing them. Plain `npm run doctor` does not require Delve.

Run commands from the repository root. Scripts resolve project paths relative
to their own module URL, use argument arrays (including paths with spaces), and
invoke npm's JavaScript CLI through Node rather than a Windows command shell.

## Development

`npm run dev` owns one development session:

1. Check prerequisites, then acquire an exclusive ownership directory under `.local/tooling/dev.lock`.
2. Refuse occupied loopback ports rather than kill another process or auto-select
   another port.
3. Start root-installed Vite with `web` as its working directory, bound to
   `127.0.0.1:19329` with strict port selection.
4. Compile `go build -tags dev ./cmd/cvtranslate` into `.local/dev` and run it with
   `--port=19328 --data-dir=<root>/.local/dev --vite-url=http://127.0.0.1:19329`.
5. Check only `/healthz` for backend readiness. The Go service, not Node or Vite,
   opens the local browser management page after readiness. The same address
   can be opened directly in other local browsers without a session exchange.

The development build does not need embedded `web/dist` files. It proxies Vite
through the Go origin and retains exact Host/Origin and Fetch Metadata checks.
Production defaults to port 19327 and its own data/credential namespace.
Do not change the development
data path to your production database for convenience.

`npm run dev -- --headless` uses the same services but passes `--no-open` and
`--no-native` to Go, for HTTP-only development checks without browser or desktop
side effects. It uses the same credential-free browser API and origin checks.

Ctrl+C or a child failure stops only owned child processes. Each long-running
child has a separate IPC-connected supervisor, which terminates its child tree
if the parent disappears during setup or normal operation. On Windows cleanup
uses `taskkill /PID <live-owned-child> /T /F`; it never discovers victims by
executable name, port or a stale PID file. POSIX cleanup targets the owned process
group. A parent-liveness watcher also stops the helper after its launching parent
exits. Force-killing the entire supervisor chain or an OS crash cannot guarantee
graceful cleanup; stale ownership fails closed instead of guessing a PID.

## F5 Go debugging

Select **CVTranslate: Dev**. The pre-launch process task runs
`node scripts/dev.mjs --prepare-debug`, owns Vite, and emits
`CVTRANSLATE_DEBUG_READY` only after Vite responds. Its background problem matcher
allows the Go debugger to launch with `buildFlags: -tags=dev` and the same
development ports/data path. The debugger owns the Go process; the helper never
kills it.

The pre-launch task checks Delve before starting Vite. Install it explicitly using
the Go extension's **Go: Install/Update Tools** command (select `dlv`), or run
`go install github.com/go-delve/delve/cmd/dlv@latest` and ensure the resulting
GOBIN (or GOPATH/bin) is on PATH. Restart VS Code after changing PATH. Tooling
does not install it silently. Frontend changes use HMR; Go changes require a debug
restart. Frontend breakpoints use browser DevTools/source maps, not a second IDE
debug adapter.

The post-debug task runs `node scripts/dev.mjs --stop-debug`. It reads a random
per-session control token from local ownership metadata and authenticates to the
helper's ephemeral loopback control endpoint. Host, method, session and token
are checked; browser-origin requests are rejected. No token is printed or put in
process arguments. Ownership files use restrictive POSIX modes; on Windows keep
the checkout under a private user directory with appropriate inherited ACLs.
Development control does not protect against another process running as the
same OS user.

Ordinary dev and F5 cannot run concurrently in one checkout. Duplicate launches
of `npm run dev` fail without terminating the existing owner.

F5 takes over instead. Before acquiring ownership, `--prepare-debug`:

1. Stops a live owner of either mode through its authenticated control endpoint.
2. Removes the lock when its owner is unreachable or its metadata is invalid.
3. Terminates listeners on 19329/19328 only when the executable lies inside this
   checkout (for example a leftover `__debug_bin` or `.local/dev` binary) or the
   command line runs this checkout's Vite entry. Windows uses
   `taskkill /PID <pid> /T /F`; POSIX sends SIGTERM, then SIGKILL after 3 seconds.
   Any other listener fails the launch and is never terminated.

Stale PIDs from lock metadata are never used. Do not delete `.local/dev` to
resolve an ownership conflict.

## Automated checks

`npm run check` runs the shared pipeline:

- Read-only prerequisite checks; macOS requires CGO and Apple's compiler tools.
- Read-only SVG/native icon synchronization checks (`npm run icons -- --check`).
- Node's focused tooling tests (ownership, cancellation, argument safety,
  archive whitelist, notices, checksums and architecture checks).
- Web workspace `typecheck`, `lint`, `test`, then `build`.
- Read-only `gofmt -l` verification (no automatic source edits).
- Architecture import checks plus `go vet` and `go test` for production and `dev`
  build tags. Domain/provider dependencies on storage/platform/server fail,
  including transitive imports.
- Current-host production and development Go compilation into a unique temporary
  directory, then `--version --no-native --no-open` smoke checks.

The version path must return before initializing a database, native integrations
or a browser. Tests must use fakes; no shortcut registration, clipboard mutation,
paid provider calls or browser automation belongs in this pipeline. Build/test
success is not native/UI acceptance. macOS runtime checks require a macOS host.

For tooling-only edits, the focused command is:

```text
node --test scripts/tests/lifecycle.test.mjs scripts/tests/release.test.mjs
```

## Application icons

Edit `web/public/icon.svg`, then run `npm run icons` to regenerate the monochrome
SVG and embedded native PNGs. Keep these source assets in version control so
Go dev/F5 builds need no new prerequisite. The rasterizer is development-only;
the WebUI consumes SVG directly. See [icon maintenance](icons.md).

## Packaging

`npm run build` calls the same pipeline once, then compiles a version-stamped
production executable and stages a current-host archive. See
[release instructions](../release.md) for contents, notices, signing limitations
and manual acceptance. It does not call `npm run check` recursively or duplicate
the frontend build. A build never modifies application data, uploads artifacts,
initializes Git or installs tools.

## Continuous integration

`.github/workflows/build.yml` triggers on pushes and pull requests to `main` that
touch build inputs (source, scripts, lockfiles, `LICENSE` and packaged notices);
documentation-only and editor-config changes are skipped. It can also be started
manually.

1. `node scripts/release-version.mjs` computes the version from Git tags: the
   `major.minor` in `package.json` plus the next unused patch among `vX.Y.Z` tags
   (the `package.json` patch is the floor). A `package.json` line older than an
   existing tag fails the run.
2. Windows (CGO off) and macOS (CGO on) runners run `npm ci`, `go mod download`
   and `node scripts/build.mjs --release-version=X.Y.Z`, then upload the release
   directory as a 14-day workflow artifact.
3. Only for pushes to `main`, after both builds succeed, a release job creates
   tag `vX.Y.Z` on the built commit and a public GitHub Release containing both
   ZIPs, a combined `SHA256SUMS` and generated notes. Pull requests and manual
   runs never publish. The in-app update check reads this latest release.

CI never commits to the repository, so `package.json` keeps the release line,
not the latest patch. Change only `major.minor` there to start a new line, for
example `0.2.0`. Pushes to `main` queue rather than cancel, so tags are computed
sequentially. A failed build publishes nothing and its version is reused by the
next push. Releases are unsigned and unnotarized. CI is compile/test evidence
only, not desktop or UI acceptance.

`.gitattributes` forces LF in the working tree so the read-only `gofmt` gate
passes on Windows checkouts regardless of `core.autocrlf`.
