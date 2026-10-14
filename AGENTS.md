# CVTranslate development instructions

Read CONTRIBUTING.md and docs/architecture.md before changing this project.
Read docs/development/provider-guide.md for any provider or provider-contract change.
Read docs/development/feature-checklist.md before adding a feature.
Read docs/development/workflows.md before changing tooling or release behavior.

- Windows/macOS desktop only. Do not create additional clients or themes.
- Keep provider, domain, storage, platform and HTTP responsibilities separate.
- Preserve user changes and data. Never reset databases to implement a migration.
- Parallel edits are expected. Reload affected files before each patch and merge
  against the latest contents without pausing or asking for confirmation merely
  because a file changed. If a patch no longer matches, reread and adapt it to
  the current code, then verify the integrated result.
  Preserve unrelated work; stop only for a destructive or materially ambiguous
  conflict that cannot be integrated safely.
- No paid calls, UI automation, permission changes or publishing without authorization.
- Keep dev, F5, check and build working; report actual checks and unverified platforms.
- Use focused regression tests, descriptive names, and the repository lockfiles.
- Keep WebUI copy concise: no tutorials, slogans or redundant explanations.
  Preserve concise errors, privacy/billing warnings and destructive confirmations.
  Follow the WebUI interaction conventions in CONTRIBUTING.md.
- WebUI text must never be smaller than 12 CSS px. Keep explicit spacing between
  controls/content and align the top/bottom edges of peer cards in a visual row.
  Vertically center buttons, icons, text and counters in control-only rows;
  inline helper text must not add top/bottom margins that offset alignment.
  For labeled fields mixed with actions, align actual control top/bottom edges,
  not whole label/input wrappers; reuse shared heights and preserve wrapped rows.
  Follow CONTRIBUTING.md's toolbar and form-row alignment contract.
  Follow CONTRIBUTING.md's typography, spacing and card-layout requirements.
- Follow CONTRIBUTING.md's WebUI visual quality baseline: clear hierarchy,
  readable contrast, consistent controls, resilient content, stable feedback,
  visible keyboard focus and unobstructed actions. Reuse shared styles/components.
- Enforce CONTRIBUTING.md's cross-page consistency contract: use the shared
  page width/gutters and visual system; no page-specific wrapper width limits
  or equivalent-control variants without explicit approval.
- Never weaken authentication, secret handling or paste guards for convenience.
- Material architecture/scope/security changes require confirmation and updated topic docs.
