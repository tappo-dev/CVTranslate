# Contributing

CVTranslate is a local Windows/macOS translation application. Start with
[architecture](docs/architecture.md) and [workflows](docs/development/workflows.md).
Documentation is authoritative by topic; editor rules link here rather than
maintaining competing copies of the design.

## Change process

1. State the intended behavior and affected platforms/contracts.
2. Review the [feature checklist](docs/development/feature-checklist.md).
3. Implement the smallest coherent change across all direct callers.
4. Add regression coverage for meaningful risks; do not add tests that merely
   repeat the implementation or contact paid services.
5. Run `npm run check`. Packaging changes also require `npm run build`.
6. Update relevant usage and development documentation.
7. Report executed checks, failures and unverified behavior separately.

## Code conventions

- Go: gofmt, explicit error handling, propagated contexts, descriptive names.
- TypeScript: strict types, semantic controls, no unexplained `any` or ignores.
- Domain/provider code cannot import platform, server or storage implementations.
- One translation service serves HTTP and global shortcuts.
- New dependencies must have an actual purpose and committed lock entries.
- Secrets, source text and translated text do not belong in diagnostic logs.
- Preserve existing migrations. Add a new ordered migration for persisted changes.
- Do not silently retry billable calls, switch providers or elevate privileges.
- Do not commit generated release packages, user data or credentials.

## WebUI copy and interactions

- Prefer clear labels and short status messages. Do not add instructional
  paragraphs, slogans, self-evident tips or repeated explanations of controls.
  For example, use "Saved" rather than a paragraph explaining how saving works.
- Keep actionable errors, permission limitations, privacy/billing warnings and
  irreversible-action confirmations, briefly and next to the relevant action.
  Detailed protocols, examples and usage guidance belong in documentation.
- Use the shared Radix UI Tabs, Toast and AlertDialog integrations for interactive
  primitives rather than implementing focus, keyboard or notification lifecycles
  by hand. Keep native form controls where they already meet the need.
- Keep settings drafts above their tab panels. Switching tabs must not save,
  discard edits, trigger requests to providers or leave voice previews playing.
  Show Save settings and Discard only while dirty. Save applies the complete
  draft; Discard restores saved values across tabs. Never autosave settings drafts.
- Automatic translation must debounce input, respect IME composition, avoid
  duplicate submissions and stale results, and never silently retry paid calls.

## WebUI typography, spacing and cards

- All rendered text must be at least **12 CSS px** at the application's normal
  scale, without exceptions for badges, hints, placeholders, metadata, counters,
  disabled controls or variable buttons. Use `--font-size-small` for secondary
  text and larger sizes for body text/headings. Do not use relative reductions,
  `zoom` or transforms to evade the minimum. User-controlled browser zoom is
  outside application styling; never override it.
- Every layout must deliberately provide padding, margin or gap between distinct
  controls and content groups. Use the shared spacing tokens: 8px between nearby
  controls/labels, 12px between a field and its auxiliary actions, 20px between
  fields, and 24px between sections/cards and for card content padding. A shared
  component may own the spacing; do not add blanket margins to every element.
- Textareas must not touch variable/action rows or the next field. Button groups
  and metadata must keep their gaps when wrapping. Allow content to wrap instead
  of shrinking fonts, overlapping controls or clipping text to fit.
- Inside a control or a control-only row, buttons, icons, labels, badges and
  counters must be **vertically centered** with `align-items: center`. Each
  wrapped row follows the same rule. Remove inherited top/bottom margins from
  inline helper text or counters; use the row's gap instead of offsets,
  transforms or manual padding to fake alignment. This is not a rule to center
  a whole labeled field against a standalone button; use the toolbar contract
  below. It also does not replace the equal-height rule for peer cards.
- Peer cards in the same visual row must align at **both top and bottom**.
  Use a shared Grid/Flex stretch layout, consistent inner padding and aligned
  content edges. List/editor columns must share their outer bounds; stacked
  list cards keep their normal gaps. Empty and editing states follow the same
  alignment. Do not fake equal height with fixed screenshot/device dimensions,
  spacer elements or hidden content.
- Review these invariants whenever editing WebUI. Keep the typography policy
  test passing; source/unit checks do not replace authorized browser layout
  acceptance at the supported desktop viewport sizes.

## WebUI toolbar and form-row alignment

- Align the **actual control boxes**, not just their wrappers. Search boxes,
  selects, native date inputs and adjacent action buttons in the same row must
  share both top and bottom edges. Reuse the shared `--control-height` (42px)
  and `.toolbar` styles for regular toolbars; do not mix regular/compact sizes
  in one control row. Account for border-box sizing, padding, line-height and
  native input defaults, not just `min-height`.
- Keep persistent labels above fields with the same label-to-control gap. Use
  `.toolbar-field` for label/control pairs. A row mixing labeled fields and
  unlabeled actions must align their control edges with a shared Grid control
  row or a bottom-aligned Flex field row. Do not center standalone actions
  against the combined height of a label and its input (or a nested form).
  Keep the action row and each button's icon/text centered internally using
  `.button-row.toolbar-actions`.
- Approved settings-only exception: use label-left/control-right rows in
  full-width cards, with controls about 360px wide. This replaces above-control
  labels for those settings rows only, not the shared page width/gutters or the
  toolbar control-edge alignment contract. Preserve label associations and wrap
  without overflow. Settings tabs share one draft; Save/Discard appear only when
  dirty and sit in normal document flow (not bottom-sticky) so they never cover
  the last control. History clearing stays a separately confirmed action.
- Composite search controls own one outer border and height; their input and
  submit button fit inside it without adding a second control height. Adjacent
  icon-only actions use the same row height and a square hit area. Disabled,
  loading and cleared states must not change those dimensions.
- At supported desktop widths, wrap whole label/control pairs and related
  action groups with explicit gaps. Preserve alignment on every resulting row;
  do not squeeze fields, detach labels, shrink icons/text or overflow the page.
  Keep validation/help content outside the aligned control row. Never compensate
  with blank labels, spacer elements, hidden text, offsets or transforms.
- When changing these rows, review search/select/date/icon/text controls together,
  including empty/populated fields, disabled actions and wrapping. Keep focused
  rendered-DOM/CSS regression coverage for shared heights, alignment and inherited
  margins. Those checks are not browser geometry or visual acceptance; the latter
  still requires authorization and separate reporting.

## WebUI visual quality baseline

These requirements apply to the Windows/macOS desktop light and dark WebUI.
Reuse shared styles/components; do not introduce a new theme, client or visual
system for an individual page. They are implementation/review requirements,
not a claim that every existing screen has passed visual acceptance.

- **Themes:** use the shared semantic color tokens in `styles.css`, including
  editor content, portals, native controls and feedback states. Light is the
  default; the topbar toggle stores only the theme in browser-local storage,
  independently of application settings drafts. Do not remount pages/editors,
  submit translations or save settings when changing themes.
- **Cross-page consistency:** all pages must use the shared `.page-content`
  wrapper as the single owner of page width, outer padding and centering.
  Page-specific wrappers must fill that available width, not introduce their
  own fixed width, narrower maximum width or different outer gutters. Page
  headings, full-width tabs/toolbars, content regions and save bars must share
  the same left/right content edges at the same desktop viewport width.
  Settings underline tabs are left-aligned with content-width triggers inside
  this shared width; they do not introduce a narrower page wrapper.
  Internal columns and individual controls may differ according to function,
  but must not narrow the overall page. Typography, spacing, cards, equivalent
  controls and interaction states must follow the same shared visual system;
  change shared styles instead of adding page-local variants for equivalent
  UI. Any deliberate page-level exception requires explicit approval and a
  documented functional reason. Review affected pages against sibling pages,
  not only in isolation; keep focused width/style regression coverage, and do
  not treat it as browser visual acceptance.
- **Hierarchy and restraint:** distinguish page headings, section headings,
  body text and secondary metadata consistently. Keep body/form text normally
  14-16px; reserve 12px for secondary text. Use comfortable line height (normally
  1.5-1.8 for prose), left-align reading/form content, and avoid centering long
  paragraphs. Group related content with whitespace before adding borders or
  nested cards. No decorative gradients, heavy shadows or oversized empty areas
  without a functional reason.
  Remove the redundant topbar breadcrumb globally and retain each page H1.
  Settings section headings have no decorative icons.
- **Readable contrast:** meet WCAG AA contrast for enabled text (4.5:1 for
  normal text, 3:1 for qualifying large text) and 3:1 for essential control
  boundaries, state indicators and focus indicators against adjacent colors.
  Secondary text and placeholders must not fade into the background. Disabled
  controls must remain identifiable/readable, not look like missing content.
  Use the shared `--muted` token (`#5f727a` in light, `#a0b3bb` in dark) and a
  readable disabled background; do not fade disabled controls with opacity.
  Never convey errors, selection or status through color alone.
- **Consistent components:** equivalent controls share heights, padding,
  borders, radii, typography and state styling across pages. Reuse existing
  regular controls at 42px high with 14px text. Preserve purposeful existing
  compact/icon-control sizes; normally keep buttons >=36px high and compact
  icon-button hit areas >=32px square, independent of glyph size. Use the existing
  icon library with consistent size/stroke for equivalent roles; no mixed emoji
  or unrelated icon styles. Icon-only actions need an accessible name and a
  concise tooltip available on hover/focus; essential actions must not depend
  on recognizing an ambiguous icon.
- **Action hierarchy and forms:** use one visually primary action per action
  group, quieter secondary actions and distinct destructive actions. Keep action
  order/placement consistent and separate destructive actions from routine ones.
  Inputs need persistent labels, not placeholder-only labels. Put validation
  next to the relevant field and associate it accessibly; do not add permanent
  explanatory prose when the label is sufficient.
- **Content resilience:** within supported desktop widths, long names, URLs,
  translations and validation errors must not overlap controls, escape cards or
  force page-wide horizontal scrolling. Wrap first; truncate only noncritical
  summaries with a keyboard-accessible way to read/copy the full value. Never
  truncate required warnings/errors or the actual translation. Preserve user
  text line breaks. Do not shrink type or hide content to force a layout to fit.
- **Stable states:** loading, empty, success, error, selected, read-only and
  disabled states must be deliberate and visually distinct where applicable.
  Keep button dimensions/action placement stable when labels change or spinners
  appear. Feedback must not unexpectedly shift controls under the pointer or
  steal focus. Empty states stay concise; use toasts for transient feedback,
  inline errors for recovery, and dialogs only when a decision is required.
- **Focus and overlays:** preserve a clearly visible keyboard focus indicator;
  hover is not a substitute for focus. Dialogs/popovers must fit the viewport,
  keep actions reachable and follow a consistent stacking order. Sticky save
  bars and notifications must not cover the last fields, errors or focused
  controls; reserve space or reposition them. Prefer normal page scrolling to
  unnecessary nested scroll regions. Reuse library focus/keyboard behavior.
- **Motion:** use short, subtle transitions only to clarify state changes.
  Avoid decorative looping, flashing, bouncing or layout-shifting animations,
  and respect `prefers-reduced-motion`.

For affected components, review long/empty content, wrapped rows, validation,
loading and keyboard focus as applicable. Review the current supported desktop
widths, not an invented mobile target. Browser/visual acceptance still requires
authorization; report it separately from source checks and builds.

## Completion standard

Handle applicable loading, success, error, cancellation and recovery states.
Keep `doctor`, `dev`, F5, `check` and `build` usable. New environment variables,
assets and build dependencies must be represented in the shared tooling.
Build success is not desktop/UI acceptance. macOS acceptance requires macOS.

Scope expansion, destructive migrations, changed security boundaries and major
architecture decisions require approval before implementation. Record the
resulting contract in the relevant topic document (architecture, development
guides or usage), not in a separate change log. Local tooling does not depend
on Git or a hosting service. CI runs the same build with a tag-derived version
and publishes each successful push to `main` as a GitHub Release.
