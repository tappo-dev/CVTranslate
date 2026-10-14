# Application icons

`web/public/icon.svg` is the editable source for the sidebar, browser favicon
and native tray branding: three stacked keycaps for the triple Ctrl+C /
Command+C gesture, a "C" cut out of the front key and a spark for translation.
It uses outlined geometry and masks rather than a font or an embedded bitmap.
The WebUI uses the SVG directly; its
adjacent application name provides the sidebar link's accessible name, so the
image has empty alt text.

After editing the source, run from the repository root:

```text
npm run icons
npm run check
```

The generator uses the locked development-only `@resvg/resvg-js` dependency.
It does not load system fonts, contact services or install native tools.
Commit the SVG source and its generated assets together:

- `web/public/icon-template.svg`: black silhouette with transparent cutouts,
  derived by recoloring every fill after `<defs>`. Do not edit it separately.
- `internal/platform/icons/tray-windows.png`: 64px color image. Windows creates
  an owned HICON directly from these embedded PNG bytes at its small-icon size.
  The adapter destroys that handle after removing the notification entry,
  including startup failure and normal shutdown paths.
- `internal/platform/icons/tray-macos.png`: 36px template image, displayed at
  18pt for Retina support. AppKit owns the menu-bar tint. Native initialization
  copies the embedded bytes before returning to Go; no Go pointer is retained.

Keep the 32-unit viewBox, put cutout masks inside `<defs>`, and give visible
shapes hex `fill` colors; the generator recolors those fills to derive the
template.
Both PNGs are compiled into the executable, with no runtime file lookup or SVG
renderer dependency. Existing Go dev/F5 builds therefore need no generation
step unless the source icon changes. `npm run icons -- --check`, also included
in the shared check/build pipeline, fails on stale or missing generated assets
without rewriting them.

This changes the existing desktop WebUI, Windows tray and macOS menu-bar image
only, not executable/Finder bundle icons, client scope or WebUI themes. Native
open/quit actions and Explorer recovery are unchanged. Refresh the webpage for
the WebUI asset and restart the rebuilt native process for its tray asset.

Asset checks and compilation are not browser or native visual acceptance.
Windows tray sizing/appearance and macOS AppKit compilation/appearance require
their respective hosts and separately authorized desktop acceptance.
