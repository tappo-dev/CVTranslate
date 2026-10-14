import { describe, expect, it } from "vitest";
import type { Hotkeys } from "./types";
import {
  recordHotkey,
  validateHotkeys,
  validatePersona,
  validateProviderEndpoint,
  validateTemplates,
  type KeyInput,
} from "./validation";

describe("provider endpoint validation", () => {
  it("accepts HTTPS without a warning", () => {
    expect(validateProviderEndpoint("https://example.com/v1")).toEqual({
      error: "",
      warning: "",
    });
  });
  it.each([
    "http://example.com:8080/v1",
    "http://192.168.1.10:8080/v1",
    "http://[2001:db8::1]:8080/v1",
    "http://localhost:8080/v1",
    "http://127.0.0.1:8080/v1",
    "http://[::1]:8080/v1",
    "http://example.com/v1%3F%23",
  ])("accepts HTTP with only a warning: %s", (endpoint) => {
    const result = validateProviderEndpoint(endpoint);
    expect(result.error).toBe("");
    expect(result.warning).not.toBe("");
  });
  it.each([
    "",
    "/v1",
    "http://",
    "http://user:secret@example.com/v1",
    "https://user:secret@example.com/v1",
    "http://example.com/v1?key=secret",
    "https://example.com/v1?key=secret",
    "http://example.com/v1?",
    "http://example.com/v1#fragment",
    "https://example.com/v1#fragment",
    "http://example.com/v1#",
    "file:///tmp/test",
    "ftp://example.com/v1",
  ])("still rejects invalid or unsafe endpoints: %s", (endpoint) => {
    expect(validateProviderEndpoint(endpoint).error).not.toBe("");
  });
});

describe("optional persona validation", () => {
  it("accepts empty configuration and literal template-like text", () => {
    expect(validatePersona("")).toBe("");
    expect(validatePersona(" \n")).toBe("");
    expect(validatePersona("乐观年轻人 {{text}} {{unknown}} {literal}")).toBe("");
  });
  it("enforces the server's Unicode code-point limit", () => {
    expect(validatePersona("😀".repeat(4000))).toBe("");
    expect(validatePersona("😀".repeat(4001))).not.toBe("");
  });
});

describe("strict prompt validation mirrored from the server", () => {
  it("accepts language variables and exactly one user text variable", () => {
    expect(
      validateTemplates(
        "Translate {{ source_language }} to {{target_language}}",
        "{{ text }}",
      ),
    ).toBe("");
    expect(validateTemplates("", "{{target_language}}: {{text}}")).toBe("");
  });
  it.each([
    ["{{text}} {{target_language}}", "{{text}}"],
    ["{{target_language}}", "{{text}}{{text}}"],
    ["{{source_language}}", "{{text}}"],
    ["{{target_language}}", "{{unknown}} {{text}}"],
    ["{{target_language}}", "{text} {{text}}"],
    ["{{target_language}}", "{{text}} }"],
    ["{{target_language}}", "{{text}"],
    ["{{target_language}}", "{{{text}}}"],
  ])(
    "rejects unsafe or structurally invalid template %s / %s",
    (system, user) => {
      expect(validateTemplates(system, user)).not.toBe("");
    },
  );
  it("counts Unicode code points rather than UTF-16 units", () => {
    expect(
      validateTemplates("😀".repeat(16000), "{{target_language}}{{text}}"),
    ).toBe("");
    expect(
      validateTemplates("😀".repeat(16001), "{{target_language}}{{text}}"),
    ).not.toBe("");
  });
});

const keyEvent = (values: Partial<KeyInput>): KeyInput => ({
  key: "t",
  ctrlKey: false,
  metaKey: false,
  shiftKey: false,
  altKey: false,
  ...values,
});
describe("global shortcut trigger validation", () => {
  const emptyHotkeys: Hotkeys = { openUi: "" };

  it("allows a disabled or ordinary open-WebUI shortcut", () => {
    expect(validateHotkeys(Object.values(emptyHotkeys), "windows")).toBe("");
    expect(validateHotkeys(["Ctrl+Space"], "windows")).toBe("");
    expect(validateHotkeys(["Cmd+Space"], "darwin")).toBe("");
  });

  it("reserves Windows Ctrl+C for the fixed repeated-copy gestures", () => {
    expect(validateHotkeys([" control + c "], "windows")).toContain("固定");
  });

  it.each(["Cmd+C", " command + c ", " CMD + C "])(
    "reserves macOS copy shortcut %s including aliases",
    (shortcut) => {
      expect(validateHotkeys([shortcut], "darwin")).toContain("普通 Command+C");
    },
  );

  it("does not reserve modified copy shortcuts or other platforms", () => {
    expect(validateHotkeys(["Cmd+Shift+C"], "darwin")).toBe("");
    expect(validateHotkeys(["Cmd+C"], "windows")).toBe("");
    expect(validateHotkeys(["Cmd+C"], "linux")).toBe("");
  });

  it.each([
    ["Ctrl+C*3", "windows"],
    ["Ctrl+C*4", "darwin"],
    ["Cmd+C*3", "darwin"],
    ["Command+C*4", "darwin"],
  ])("rejects persisted repeated-copy shortcut %s on %s", (shortcut, platform) => {
    expect(validateHotkeys([shortcut], platform)).toContain("固定功能");
  });

  it.each([
    ["Ctrl+Alt+T", " alt + control + t "],
    ["Cmd+Shift+T", "shift+command+t"],
    ["Alt+T", "Option+t"],
    ["Win+T", "Super+t"],
  ])("rejects normalized duplicates %s / %s", (first, second) => {
    expect(validateHotkeys([first, "", second], "windows")).not.toBe("");
  });
});

describe("global shortcut recording", () => {
  it.each(["c", "Delete", "Escape"])("ignores auto-repeated %s", (key) => {
    expect(recordHotkey(keyEvent({ key, ctrlKey: true, repeat: true }), false))
      .toEqual({});
  });
  it("uses platform-specific primary modifiers", () => {
    expect(
      recordHotkey(keyEvent({ ctrlKey: true, altKey: true }), false),
    ).toEqual({ value: "Ctrl+Alt+T" });
    expect(
      recordHotkey(keyEvent({ metaKey: true, shiftKey: true }), true),
    ).toEqual({ value: "Cmd+Shift+T" });
    expect(recordHotkey(keyEvent({ metaKey: true }), false).error).toBeTruthy();
  });
  it("does not capture ordinary typing or a bare modifier", () => {
    expect(recordHotkey(keyEvent({}), false).error).toBeTruthy();
    expect(
      recordHotkey(keyEvent({ shiftKey: true }), false).error,
    ).toBeTruthy();
    expect(
      recordHotkey(keyEvent({ key: "Control", ctrlKey: true }), false),
    ).toEqual({});
  });
  it("cancels with Escape and explicitly disables with Delete", () => {
    expect(recordHotkey(keyEvent({ key: "Escape" }), false)).toEqual({
      cancel: true,
    });
    expect(recordHotkey(keyEvent({ key: "Delete" }), false)).toEqual({
      value: "",
    });
  });
  it("rejects unsupported punctuation and accepts modified function keys", () => {
    expect(
      recordHotkey(keyEvent({ key: "+", ctrlKey: true }), false).error,
    ).toBeTruthy();
    expect(recordHotkey(keyEvent({ key: "F12", altKey: true }), false)).toEqual(
      { value: "Alt+F12" },
    );
  });
  it("records Ctrl+Space and normalizes other supported navigation keys", () => {
    expect(recordHotkey(keyEvent({ key: " ", ctrlKey: true }), false)).toEqual(
      { value: "Ctrl+Space" },
    );
    expect(recordHotkey(keyEvent({ key: "Enter", altKey: true }), false)).toEqual(
      { value: "Alt+Return" },
    );
    expect(recordHotkey(keyEvent({ key: "ArrowLeft", ctrlKey: true }), false)).toEqual(
      { value: "Ctrl+Left" },
    );
  });
});
