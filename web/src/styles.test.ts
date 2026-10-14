// @vitest-environment jsdom
import { readFileSync, readdirSync } from "node:fs";
import { join } from "node:path";
import { describe, expect, it, vi } from "vitest";
import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { fireEvent, render, within } from "@testing-library/react";
import { FeedbackProvider } from "./feedback";
import { HistoryPage } from "./HistoryPage";
import { PromptsPage } from "./PromptsPage";
import { SettingsPage } from "./SettingsPage";
import { createTestAppState } from "./speech-test-fixtures";

vi.mock("./shortcut-status", async (importOriginal) => ({
  ...(await importOriginal<typeof import("./shortcut-status")>()),
  watchPlatformStatus: () => ({ stop: vi.fn(), refresh: vi.fn() }),
}));

// Vitest stubs CSS imports unless CSS processing is enabled; read the source instead.
const sourceDirectory = join(process.cwd(), "src");
const stylesheets = Object.fromEntries(
  readdirSync(sourceDirectory, { recursive: true, encoding: "utf8" })
    .filter((filename) => filename.endsWith(".css"))
    .map((filename) => [
      filename,
      readFileSync(join(sourceDirectory, filename), "utf8"),
    ]),
);

function collectStyleRules(rules: CSSRuleList): CSSStyleRule[] {
  return Array.from(rules).flatMap((rule) => {
    if (rule.type === CSSRule.STYLE_RULE) return [rule as CSSStyleRule];
    const nestedRules = (rule as CSSGroupingRule).cssRules;
    return nestedRules ? collectStyleRules(nestedRules) : [];
  });
}

describe("WebUI minimum typography contract", () => {
  it("keeps every declared text size and size token at least 12 CSS pixels", () => {
    expect(Object.keys(stylesheets).length).toBeGreaterThan(0);
    for (const [filename, stylesheet] of Object.entries(stylesheets)) {
      const element = document.createElement("style");
      element.textContent = stylesheet;
      document.head.append(element);
      try {
        expect(element.sheet, filename).not.toBeNull();
        const rules = collectStyleRules(element.sheet!.cssRules);
        expect(rules.length, filename).toBeGreaterThan(0);
        const minimumToken = rules
          .filter((rule) => rule.selectorText === ":root")
          .map((rule) =>
            rule.style.getPropertyValue("--font-size-small").trim(),
          )
          .filter(Boolean);

        for (const rule of rules) {
          const shorthand = rule.style.getPropertyValue("font").trim();
          if (shorthand) {
            expect(
              shorthand,
              `${filename}: ${rule.selectorText}; use explicit font-size`,
            ).toBe("inherit");
          }
          for (const property of Array.from(rule.style)) {
            if (
              property !== "font-size" &&
              !property.startsWith("--font-size-")
            )
              continue;
            const value = rule.style.getPropertyValue(property).trim();
            if (property === "font-size" && value === "inherit") continue;
            const context = `${filename}: ${rule.selectorText}; ${property}: ${value}`;
            if (value === "var(--font-size-small)") {
              expect(minimumToken, context).toEqual(["12px"]);
            } else {
              expect(value, context).toMatch(/^\d+(?:\.\d+)?px$/);
              expect(Number.parseFloat(value), context).toBeGreaterThanOrEqual(
                12,
              );
            }
          }
        }
      } finally {
        element.remove();
      }
    }
  });
});

describe("shared theme colors", () => {
  function measureLuminance(color: string) {
    expect(color).toMatch(/^#[0-9a-f]{6}$/i);
    const [red, green, blue] = [1, 3, 5].map((offset) => {
      const channel = Number.parseInt(color.slice(offset, offset + 2), 16) / 255;
      return channel <= 0.04045 ? channel / 12.92 : ((channel + 0.055) / 1.055) ** 2.4;
    });
    return 0.2126 * red + 0.7152 * green + 0.0722 * blue;
  }

  it.each(["light", "dark"])("keeps %s theme text and control boundaries readable", (theme) => {
    const stylesheet = document.createElement("style");
    stylesheet.textContent = stylesheets["styles.css"];
    document.head.append(stylesheet);
    document.documentElement.dataset.theme = theme;
    try {
      const tokens = getComputedStyle(document.documentElement);
      expect(tokens.colorScheme).toBe(theme);
      function expectContrast(foreground: string, background: string, minimum = 4.5) {
        const foregroundLuminance = measureLuminance(tokens.getPropertyValue(foreground).trim());
        const backgroundLuminance = measureLuminance(tokens.getPropertyValue(background).trim());
        const ratio = (Math.max(foregroundLuminance, backgroundLuminance) + 0.05) /
          (Math.min(foregroundLuminance, backgroundLuminance) + 0.05);
        expect(ratio, `${theme}: ${foreground} on ${background}`).toBeGreaterThanOrEqual(minimum);
      }
      for (const surface of ["--surface", "--surface-muted", "--surface-subtle", "--surface-hover", "--page-background"]) {
        for (const text of ["--text", "--text-secondary", "--button-text", "--muted"]) {
          expectContrast(text, surface);
        }
        expectContrast("--accent", surface, 3);
      }
      for (const surface of ["--surface", "--surface-muted", "--page-background"]) {
        expectContrast("--control-border", surface, 3);
      }
      for (const state of ["danger", "info", "success", "warning", "pending"]) {
        expectContrast(`--${state}`, `--${state}-background`);
        expectContrast(`--${state}`, "--surface");
      }
      expectContrast("--accent", "--accent-light");
      expectContrast("--accent", "--surface");
      expectContrast("--on-accent", "--accent");
      expectContrast("--on-accent", "--accent-hover");
      expectContrast("--tooltip-text", "--tooltip-background");
      expectContrast("--muted", "--disabled-background");
    } finally {
      document.documentElement.removeAttribute("data-theme");
      stylesheet.remove();
    }
  });

  it("keeps topbar actions equally sized and uses theme tokens outside the palettes", () => {
    const stylesheet = document.createElement("style");
    stylesheet.textContent = Object.values(stylesheets).join("\n");
    const container = document.createElement("div");
    container.className = "topbar-actions";
    container.innerHTML = '<button class="topbar-icon-button"></button><a class="topbar-icon-link"></a>';
    document.head.append(stylesheet);
    document.body.append(container);
    try {
      expect(getComputedStyle(container).alignItems).toBe("center");
      expect(getComputedStyle(container).gap).toBe("var(--space-control)");
      for (const control of container.children) {
        const styles = getComputedStyle(control);
        expect(styles.width).toBe("32px");
        expect(styles.height).toBe("32px");
        expect(styles.minHeight).toBe("32px");
        expect(styles.alignItems).toBe("center");
      }
      // Prevent light-only literals in page/editor/portal styles. Palette contrast
      // and cascade checks are not browser geometry or visual acceptance.
      for (const rule of collectStyleRules(stylesheet.sheet!.cssRules)) {
        if (rule.selectorText.startsWith(":root")) continue;
        for (const property of Array.from(rule.style)) {
          expect(rule.style.getPropertyValue(property), `${rule.selectorText}: ${property}`)
            .not.toMatch(/#[0-9a-f]{3,8}\b|\b(?:white|black)\b|\brgba?\(/i);
        }
      }
    } finally {
      container.remove();
      stylesheet.remove();
    }
  });
});

describe("shared page width contract", () => {
  it("does not narrow settings regions inside the shared content container", () => {
    const stylesheet = document.createElement("style");
    stylesheet.textContent = Object.values(stylesheets).join("\n");
    const container = document.createElement("div");
    container.className = "page-content";
    container.innerHTML = renderToStaticMarkup(
      createElement(FeedbackProvider, {
        children: createElement(SettingsPage, {
          state: createTestAppState(),
          updateState: () => {},
        }),
      }),
    );
    document.head.append(stylesheet);
    document.body.append(container);
    try {
      const settingsRegions = container.querySelectorAll<HTMLElement>(
        ".settings-container, .settings-tabs, .settings-card, .settings-save",
      );
      expect(settingsRegions).toHaveLength(3);
      // JSDOM checks style constraints here, not browser layout dimensions.
      for (const region of settingsRegions) {
        const styles = getComputedStyle(region);
        for (const property of ["width", "inline-size"]) {
          expect(["", "auto", "100%"], `${region.className}: ${property}`)
            .toContain(styles.getPropertyValue(property));
        }
        for (const property of ["max-width", "max-inline-size"]) {
          expect(["", "none", "100%"], `${region.className}: ${property}`)
            .toContain(styles.getPropertyValue(property));
        }
      }
    } finally {
      container.remove();
      stylesheet.remove();
    }
  });
});

describe("settings control alignment", () => {
  it("keeps dirty actions full-width and standard controls aligned across settings panels", () => {
    const stylesheet = document.createElement("style");
    stylesheet.textContent = Object.values(stylesheets).join("\n");
    document.head.append(stylesheet);
    const view = render(createElement(FeedbackProvider, {
      children: createElement(SettingsPage, {
        state: createTestAppState(), updateState: () => {},
      }),
    }));
    const page = within(view.container);
    function expectRegularControl(control: Element) {
      const styles = getComputedStyle(control);
      expect(styles.boxSizing).toBe("border-box");
      expect(styles.minHeight).toBe("var(--control-height)");
      // JSDOM can retain `font: inherit` despite a later explicit font-size.
      const declaredSizes = collectStyleRules(stylesheet.sheet!.cssRules)
        .filter((rule) => control.matches(rule.selectorText))
        .map((rule) => rule.style.getPropertyValue("font-size"))
        .filter(Boolean);
      expect(declaredSizes.at(-1)).toBe("14px");
      for (const margin of [styles.marginTop, styles.marginBottom]) {
        expect(["", "0px"]).toContain(margin);
      }
      if (control.matches("select, input, .speech-preview-button")) {
        expect(styles.height).toBe("var(--control-height)");
      }
      if (control.matches("button")) expect(styles.alignItems).toBe("center");
    }
    function expectLabeledRows() {
      const rows = view.container.querySelectorAll<HTMLElement>(".settings-row:has(label)");
      expect(rows.length).toBeGreaterThan(0);
      for (const row of rows) {
        const styles = getComputedStyle(row);
        expect(styles.display).toBe("grid");
        expect(styles.gridTemplateColumns).toBe("minmax(200px, 1fr) minmax(0, 360px)");
        expect(styles.gap).toBe("var(--space-section)");
        const label = row.querySelector("label")!;
        expect(label.control).not.toBeNull();
        expect(getComputedStyle(label).alignItems).toBe("center");
        expect(getComputedStyle(label).minHeight).toBe("var(--control-height)");
        expect(getComputedStyle(row.querySelector(".settings-control")!).gap).toBe("var(--space-control)");
      }
    }
    try {
      // These are rendered cascade constraints, not browser geometry assertions.
      expect(getComputedStyle(document.documentElement).getPropertyValue("--control-height").trim()).toBe("42px");
      expectLabeledRows();
      for (const select of page.getAllByRole("combobox")) expectRegularControl(select);
      fireEvent.change(page.getByLabelText("默认目标语言"), { target: { value: "en" } });
      const saveBar = view.container.querySelector<HTMLElement>(".settings-save")!;
      expect(saveBar).not.toBeNull();
      const saveStyles = getComputedStyle(saveBar);
      // A bottom-sticky bar obscured shortcut diagnostics at 1280x800.
      expect(saveStyles.position).toBe("static");
      for (const property of ["width", "inline-size"]) {
        expect(["", "auto", "100%"]).toContain(saveStyles.getPropertyValue(property));
      }
      for (const property of ["max-width", "max-inline-size"]) {
        expect(["", "none", "100%"]).toContain(saveStyles.getPropertyValue(property));
      }
      expect(saveStyles.alignItems).toBe("center");
      expect(saveStyles.flexWrap).toBe("wrap");
      expect(saveStyles.gap).toBe("var(--space-related)");
      expect(saveStyles.marginTop).toBe("var(--space-section)");
      const actionRow = saveBar.querySelector(".button-row")!;
      expect(getComputedStyle(actionRow).alignItems).toBe("center");
      expect(getComputedStyle(actionRow).gap).toBe("var(--space-control)");
      for (const button of within(saveBar).getAllByRole("button")) expectRegularControl(button);

      fireEvent.mouseDown(page.getByRole("tab", { name: "朗读" }), { button: 0, ctrlKey: false });
      expectLabeledRows();
      for (const select of page.getAllByRole("combobox")) expectRegularControl(select);
      const voiceRows = view.container.querySelectorAll(".speech-voice-controls");
      expect(voiceRows).toHaveLength(2);
      for (const row of voiceRows) {
        expect(getComputedStyle(row).display).toBe("flex");
        expect(getComputedStyle(row).alignItems).toBe("center");
        expect(getComputedStyle(row).gap).toBe("var(--space-related)");
        expectRegularControl(row.querySelector("button")!);
      }
      const speechHelp = view.container.querySelector(".settings-speech-feedback > p")!;
      expect(getComputedStyle(speechHelp).marginTop).toBe("0px");
      expect(getComputedStyle(speechHelp).marginBottom).toBe("0px");

      fireEvent.mouseDown(page.getByRole("tab", { name: "本地服务" }), { button: 0, ctrlKey: false });
      expectLabeledRows();
      expectRegularControl(page.getByRole("spinbutton"));
      const platformInfo = view.container.querySelector(".platform-info")!;
      expect(getComputedStyle(platformInfo).fontSize).toBe("14px");
      const platformCells = platformInfo.querySelectorAll("dt, dd");
      expect(platformCells).toHaveLength(6);
      for (const cell of platformCells) {
        const styles = getComputedStyle(cell);
        expect(styles.alignItems).toBe("center");
        for (const margin of [styles.marginTop, styles.marginBottom, styles.marginLeft, styles.marginRight]) {
          expect(margin).toBe("0px");
        }
      }
    } finally {
      view.unmount();
      view.container.remove();
      stylesheet.remove();
    }
  });
});

describe("translation workspace viewport contract", () => {
  it("keeps page overflow fixed while long editor content scrolls internally", () => {
    const stylesheet = document.createElement("style");
    stylesheet.textContent = Object.values(stylesheets).join("\n");
    const container = document.createElement("div");
    container.innerHTML = `
      <main class="translate-main">
        <header class="topbar"></header>
        <div class="page-content translate-page-content">
          <div class="page-heading"></div>
          <div class="translate-page">
            <section class="card translation-workspace">
              <div class="language-bar"></div>
              <div class="editor-columns">
                <div class="editor-pane">
                  <div class="source-markdown-editor">
                    <div class="markdown-editor-body">
                      <textarea class="markdown-source-editor"></textarea>
                    </div>
                  </div>
                </div>
                <div class="editor-pane result-pane">
                  <div class="translation-output"></div>
                </div>
              </div>
              <div class="translation-actions"></div>
            </section>
            <p class="footnote"></p>
          </div>
        </div>
      </main>
    `;
    document.head.append(stylesheet);
    document.body.append(container);
    try {
      const main = container.querySelector<HTMLElement>(".translate-main")!;
      expect(getComputedStyle(main).height).toBe("100vh");
      expect(getComputedStyle(main).overflow).toBe("hidden");

      for (const selector of [
        ".translate-page-content",
        ".translate-page",
        ".translation-workspace",
        ".editor-columns",
        ".editor-pane",
        ".source-markdown-editor",
        ".markdown-editor-body",
      ]) {
        const element = container.querySelector<HTMLElement>(selector)!;
        expect(getComputedStyle(element).minHeight, selector).toBe("0");
      }

      const sourceEditor = container.querySelector<HTMLTextAreaElement>(
        ".markdown-source-editor",
      )!;
      expect(getComputedStyle(sourceEditor).overflowY).toBe("auto");

      const translationOutput = container.querySelector<HTMLElement>(
        ".translation-output",
      )!;
      expect(getComputedStyle(translationOutput).overflowY).toBe("auto");
      expect(getComputedStyle(translationOutput).minHeight).toBe(
        "var(--translation-content-min-height)",
      );
    } finally {
      container.remove();
      stylesheet.remove();
    }
  });
});

describe("history toolbar alignment", () => {
  it.each([
    { state: "empty dates", date: "", clearDisabled: true },
    { state: "populated dates", date: "2026-10-14", clearDisabled: false },
  ])("keeps labeled controls and actions aligned with $state", ({ date, clearDisabled }) => {
    const stylesheet = document.createElement("style");
    stylesheet.textContent = Object.values(stylesheets).join("\n");
    const container = document.createElement("div");
    container.innerHTML = renderToStaticMarkup(
      createElement(FeedbackProvider, {
        children: createElement(HistoryPage, {
          openTask: () => {},
          initialDateFrom: date,
          initialDateTo: date,
        }),
      }),
    );
    document.head.append(stylesheet);
    document.body.append(container);
    try {
      // Check the real DOM and cascade; JSDOM cannot measure wrapping or geometry.
      const toolbar = container.querySelector<HTMLElement>(".toolbar")!;
      const dateFilter = toolbar.querySelector<HTMLElement>(".toolbar-filters")!;
      for (const fieldRow of [toolbar, dateFilter]) {
        const styles = getComputedStyle(fieldRow);
        expect(styles.display).toBe("flex");
        expect(styles.alignItems).toBe("flex-end");
        expect(styles.flexWrap).toBe("wrap");
      }

      const fields = toolbar.querySelectorAll<HTMLElement>(".toolbar-field");
      expect(fields).toHaveLength(4);
      for (const field of fields) {
        expect(getComputedStyle(field).display).toBe("grid");
        expect(getComputedStyle(field).gap).toBe("var(--space-control)");
        const input = field.querySelector<HTMLInputElement | HTMLSelectElement>("input, select")!;
        expect(input.labels).toHaveLength(1);
        expect(input.labels![0].textContent?.trim()).not.toBe("");
      }

      const controlHeight = "var(--control-height)";
      expect(getComputedStyle(document.documentElement)
        .getPropertyValue("--control-height").trim()).toBe("42px");
      const controlBoxes = toolbar.querySelectorAll<HTMLElement>(
        ".search-box, .toolbar-field > input, .toolbar-field > select, .toolbar-actions > button",
      );
      expect(controlBoxes).toHaveLength(7);
      for (const control of controlBoxes) {
        const styles = getComputedStyle(control);
        expect(styles.boxSizing).toBe("border-box");
        expect(styles.height).toBe(controlHeight);
        expect(styles.minHeight).toBe(controlHeight);
        expect(styles.marginTop).toBe("0px");
        expect(styles.marginBottom).toBe("0px");
      }

      const actionRows = toolbar.querySelectorAll<HTMLElement>(".toolbar-actions");
      expect(actionRows).toHaveLength(2);
      for (const row of actionRows) {
        expect(getComputedStyle(row).alignItems).toBe("center");
        for (const button of row.querySelectorAll("button")) {
          expect(getComputedStyle(button).alignItems).toBe("center");
        }
      }
      expect(dateFilter.querySelector<HTMLButtonElement>("button.subtle")!.disabled)
        .toBe(clearDisabled);
      const refresh = toolbar.querySelector<HTMLButtonElement>(".toolbar-icon-button")!;
      expect(getComputedStyle(refresh).width).toBe(controlHeight);

      const searchBox = toolbar.querySelector<HTMLElement>(".search-box")!;
      expect(getComputedStyle(searchBox).alignItems).toBe("center");
      for (const child of searchBox.querySelectorAll("input, button")) {
        expect(getComputedStyle(child).height).toBe("100%");
        expect(getComputedStyle(child).minHeight).toBe("0");
      }
    } finally {
      container.remove();
      stylesheet.remove();
    }
  });
});

describe("prompt action row alignment", () => {
  it("centers the actual persona controls without helper-text margin offsets", () => {
    const state = createTestAppState();
    state.prompts = [
      {
        id: "template",
        name: "Template",
        systemTemplate: "Translate to {{target_language}}.",
        userTemplate: "{{text}}",
        persona: "",
        revision: 1,
        builtIn: false,
      },
    ];
    const stylesheet = document.createElement("style");
    stylesheet.textContent = Object.values(stylesheets).join("\n");
    const container = document.createElement("div");
    container.innerHTML = renderToStaticMarkup(
      createElement(FeedbackProvider, {
        children: createElement(PromptsPage, { state, updateState: () => {} }),
      }),
    );
    document.head.append(stylesheet);
    document.body.append(container);
    try {
      const counter = container.querySelector<HTMLElement>(".character-count");
      expect(counter).not.toBeNull();
      const row = counter!.closest<HTMLElement>(".button-row");
      expect(row).not.toBeNull();
      expect(row!.querySelectorAll("button")).toHaveLength(2);
      expect(getComputedStyle(row!).alignItems).toBe("center");
      expect(getComputedStyle(counter!).marginTop).toBe("0px");
      expect(getComputedStyle(counter!).marginBottom).toBe("0px");

      // Inline helper text must not inherit the block helper's 14px top margin.
      const helper = document.createElement("span");
      helper.className = "help-text";
      helper.textContent = "Status";
      row!.append(helper);
      expect(getComputedStyle(helper).marginTop).toBe("0px");
      expect(getComputedStyle(helper).marginBottom).toBe("0px");
    } finally {
      container.remove();
      stylesheet.remove();
    }
  });
});
