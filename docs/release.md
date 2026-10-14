# CVTranslate local release artifacts

## Create an artifact

On the intended Windows/macOS host, install the locked dependencies with
`npm ci`, run `npm run doctor`, and run `npm run build`. Node and Go target
architectures must match; cross-compilation is not used as desktop acceptance.
The shared checks run once, including the frontend build, then Go embeds the
production assets and produces the release binary.

Local builds use the version in root `package.json`. CI passes
`--release-version=X.Y.Z` instead (see [continuous integration](development/workflows.md#continuous-integration)).
Linker variables are
`main.version`, `main.commit` and `main.buildTime`. Git metadata is optional:
without a repository/commit it is explicitly `unavailable`, never invented.
The build records host architecture, compiler versions, CGO state, source
version/commit/dirty status and UTC build time in `build-manifest.json`. It does
not dump environment variables or absolute developer paths into the manifest.

Artifacts are staged in a randomly named owned directory under
`release/.staging`, verified against an exact file whitelist, then atomically
renamed to `release/cvtranslate-<version>-<platform>-<architecture>/` on the same
filesystem. A reservation prevents concurrent builds of the same target/version.
An existing final directory is an error and is never overwritten. On failure,
only that build's own staging directory/reservation are removed. After an OS
crash, inspect stale staging locks before removing them; another build may own
them. Choose a new version or intentionally move an old artifact outside the
destination before rebuilding it.

## Archive contents

The ZIP contains only:

- Windows: `cvtranslate.exe`.
- macOS: `CVTranslate.app/Contents/MacOS/cvtranslate` and `Contents/Info.plist`.
  The stable bundle ID is `com.cvtranslate.desktop`, with `LSUIElement=true` for
  a background application, not a new native main window.
- `LICENSE`, `RELEASE.md`, `THIRD-PARTY-NOTICES.txt`, `build-manifest.json`, `SHA256SUMS`.

The final output directory also contains the ZIP. Its external `SHA256SUMS`
includes that ZIP and each payload file; the copy inside the ZIP covers payload
files only, never itself. Verify the ZIP checksum before extracting, then verify
the extracted payload checksums. SHA-256 detects corruption, not publisher
identity. Windows ZIP creation uses the locked `archiver` development dependency;
macOS uses system `ditto` to preserve bundle executable permissions and metadata.

No database, `.env`, credentials, ownership token, development binary, source
checkout or `node_modules` is included. Unexpected files/directories/symlinks
abort packaging. Production web assets are embedded in the Go binary rather than
copied alongside it.

## Third-party notices and application licensing

The generated notices include actual LICENSE/COPYING/NOTICE texts from resolved
Go production modules, the Go runtime/standard library, and npm's resolved
production dependency tree. npm package metadata supplies declared license
identifiers; Go modules without such metadata are marked accordingly, while
their license text is still included. Missing upstream notice text stops the
release for review rather than substituting a guessed license. Local Go module
replacements are recorded and their resolved license texts are used. No remote
license service is contacted.

For reviewed npm packages that omit license files, the collector uses local
upstream text from `docs/third-party` only for the exact package name, version
and declared license. Packaged notices always take precedence. Supplemental
source URLs are included in the generated notices; builds do not fetch them
or modify `node_modules`. Unreviewed versions and missing or empty text still
stop packaging. See `docs/third-party/README.md` in the source checkout for
provenance and update instructions.

CVTranslate's original software and documentation use the custom **CVTranslate
Noncommercial License 1.0** in the root `LICENSE`. Noncommercial use is permitted
under its conditions; commercial use requires prior written authorization from
the relevant copyright holders. Redistribution must retain the full license,
copyright attribution and original GitHub repository link and identify material
modifications. The license does not require publication of modified source.
This is a source-available license, not an OSI-approved open source license.

The build copies the root `LICENSE` unchanged into the release payload and ZIP;
the whitelist requires it and the checksums cover it. The manifest records the
root package's `SEE LICENSE IN LICENSE` declaration. Dependency licenses remain
independent and are not replaced by the application's noncommercial terms.
Generated inventory is useful evidence, not a legal compliance certification.

The canonical upstream GitHub repository is
<https://github.com/tappo-dev/CVTranslate>, recorded in root `LICENSE`,
`README.md` and `package.json`. Before the first public release, confirm the
copyright attribution. The current collective attribution is
`CVTranslate contributors`; a personal
name, contact address or repository URL must not be inferred from another
project. These publication details require maintainer confirmation, not an
automatic guess from a fork's Git remote. Obtain legal review of the custom
terms before relying on them for commercial authorization or enforcement.
Previously built archives are not retroactively updated; rebuild after the
license and attribution are finalized, without overwriting existing artifacts.

## Running and verifying

On Windows, extract before launching `cvtranslate.exe`. On macOS, extract with a
tool that preserves executable permissions and launch `CVTranslate.app`. Normal
startup initializes production integrations and opens the local browser UI;
the default listener is `127.0.0.1:19327`. The embedded frontend requires neither
Node nor Vite on the recipient's machine. Native OS permissions may still be
required for explicitly enabled actions.

The Windows release uses the GUI subsystem to avoid a persistent console window;
development builds keep console output. Diagnostic logs are written under the
application data directory (`%APPDATA%/CVTranslate/application.log` on Windows,
`~/Library/Application Support/CVTranslate/application.log` on macOS). Run the
application again to reopen its browser. On Windows, the CVTranslate notification
icon also opens the browser; its context menu offers Open management UI and Quit
(Chinese labels). Check the taskbar's hidden-icons area if it is not visible.
Closing the browser does not quit. Tray Quit cancels pending work, releases
shortcuts and closes the service without deleting user data. On macOS, click the
CVTranslate menu-bar speech-bubble icon for the same Open management UI and Quit
actions (Chinese labels). The menu itself needs no Accessibility/Input Monitoring
grant and the app retains its existing background-only bundle configuration.
This macOS implementation must be built and accepted on a Mac; Windows checks
do not validate its native compilation or menu interaction. WebUI Settings does
not contain an application-exit button on either platform. Logs never
intentionally include original text, translated text or API keys.

For a side-effect-free identity check, invoke the executable with
`--version --no-native --no-open`. The version path must not create a database,
create a tray icon, register shortcuts, access clipboard state or open the
browser. Packaging runs this smoke check automatically; it does not run normal
application startup.

These are **unsigned, unnotarized artifacts**. No code signing, notarization or
installer creation is performed. Local builds upload nothing; only the CI
release job publishes GitHub Releases, and users replace the app manually after
the in-app update check. macOS Gatekeeper/Windows reputation warnings are possible; do not
describe this output as a trusted production installer or recommend disabling
OS security protections.

Before distributing, separately authorize and perform current-host manual
acceptance: normal startup/shutdown, local browser access, offline embedded
assets, storage persistence, credential handling and applicable native shortcut/
paste permission flows. Confirm production/development data isolation. macOS
acceptance must run on macOS, Windows acceptance on Windows. Automated Go/Web
checks and archive generation do not establish either platform's desktop/UI
acceptance or signing status.
