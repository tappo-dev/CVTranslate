# Supplemental dependency licenses

These files supply upstream license text omitted from the installed npm packages.
`scripts/lib/notices.mjs` uses them only when no license/notice file is present
and the package name, exact version and declared license match a reviewed entry.
Builds read these local files without downloading licenses or modifying
`node_modules`. Unknown versions and missing or empty license text still fail.
All entries below were reviewed on 2026-10-14; they declare MIT.

## cm6-theme-basic-light@0.2.0

- Local text: `cm6-theme-basic-light-0.2.0.LICENSE`.
- Source: [upstream LICENSE](https://github.com/craftzdog/cm6-themes/blob/fc765963a365ec4dc3b6771cea7a881350359d9a/LICENSE).
- The npm registry identifies this commit as the published package's `gitHead`.
  The source package manifest at that commit still says `0.1.0`; the published
  package says `0.2.0`. The repository, author and MIT declaration match. This
  supplement is restricted to the installed `0.2.0`, not other versions.

## format@0.2.2

- Local text: `format-0.2.2.LICENSE`.
- Attribution and license link: [release README](https://github.com/samsonjs/format/blob/4f898096759776b7c84fa7a25b13c923dadfe46e/Readme.md),
  at the npm registry's published `gitHead`.
- Terms: [author's linked MIT license](https://sjs.mit-license.org/2010-2014).
- The README supplies the 2010-2014 attribution; the linked page supplies the
  complete terms. The local transcription retains that attribution and uses
  ASCII quotation marks. The year-qualified URL avoids the site's default
  current-year attribution. npm declares MIT using its older `licenses` array.

## Highlight-mark packages

- Packages: `mdast-util-highlight-mark@1.2.2` and
  `micromark-extension-highlight-mark@1.2.0`.
- Local text: `remark-highlight-mark.LICENSE`.
- Source: [monorepo License](https://github.com/shlroland/remark-highlight-mark/blob/63a6d1cb6e34ff00d875693d5a235bdcf0f8d44d/License).
- Both package manifests at this commit match the installed versions and
  declare MIT. The root license applies to the shared upstream repository.

## react-remove-scroll-bar@2.3.8

- Local text: `react-remove-scroll-bar-2.3.8.LICENSE`.
- Source: [upstream LICENSE addition](https://github.com/theKashey/react-remove-scroll-bar/blob/7301c160fda44cb8cf2b9fdfde61efad35736196/LICENSE).
- The installed manifest and README declare MIT and identify Anton Korzunov as
  the author. Upstream explicitly added this missing license file. The registry's
  `gitHead` (`b3b1287aad81def2e2ae707274b74531b61ddbaf`) was not available through
  GitHub during review, so the source above records the reachable license-addition
  commit, not a claimed exact release commit.

## Updating dependencies

Review the actual upstream attribution and terms for any new package/version
before adding an entry. Record immutable source URLs where available and any
provenance limitations here. Keep the full terms and copyright notices; never
substitute a generic MIT template or suppress missing-license errors. Prefer
package-supplied files when an upstream release restores them. Run the release
tests and packaging checks after changes.

These notices cover dependencies only; they do not license CVTranslate or
certify redistribution compliance.
