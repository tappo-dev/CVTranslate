// @vitest-environment jsdom
import { useState } from "react";
import { act, cleanup, fireEvent, render, screen, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  $getRoot,
  $isTextNode,
  getNearestEditorFromDOMNode,
  PASTE_COMMAND,
  REDO_COMMAND,
  UNDO_COMMAND,
} from "lexical";
import { SourceMarkdownEditor, type MarkdownMode } from "./MarkdownEditor";

beforeEach(() => {
  vi.stubGlobal("DragEvent", class DragEvent extends Event {});
  vi.stubGlobal("ClipboardEvent", class ClipboardEvent extends Event {});
});
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

function PasteEditor({ onChange = vi.fn(), initialValue = "" }) {
  const [value, setValue] = useState(initialValue);
  const [mode, setMode] = useState<MarkdownMode>("markdown");
  return <SourceMarkdownEditor
    value={value}
    mode={mode}
    onModeChange={setMode}
    disabled={false}
    invalid={false}
    onChange={(markdown) => {
      setValue(markdown);
      onChange(markdown);
    }}
    onBlur={() => {}}
    onCompositionChange={() => {}}
    onSubmit={() => {}}
  />;
}

async function pasteClipboardData(
  element: HTMLElement,
  data: Record<string, string>,
  select: () => void = () => { $getRoot().selectEnd(); },
) {
  const editor = getNearestEditorFromDOMNode(element);
  if (!editor) throw new Error("Expected a mounted Lexical editor");
  const event = new ClipboardEvent("paste", { cancelable: true });
  Object.defineProperty(event, "clipboardData", {
    value: {
      types: Object.keys(data),
      files: [],
      getData: (format: string) => data[format] ?? "",
    },
  });
  await act(async () => {
    editor.update(select, { discrete: true });
    editor.dispatchCommand(PASTE_COMMAND, event);
  });
  return editor;
}

describe("Markdown clipboard import", () => {
  const markdown = "## First run\n\n1. **Microsoft Translator** is built in.\n   No API key required.\n2. Choose a provider.";
  const highlightedHtml = '<div style="font-family: Consolas"><div><span style="font-weight: bold">## First run</span></div><div><br></div><div>1. <span style="font-weight: bold">**Microsoft Translator**</span> is built in.</div><div>   No API key required.</div><div>2. Choose a provider.</div></div>';

  it.each(["plain", "code editor", "markdown MIME"])("imports %s Markdown as structure instead of escaped literal text", async (format) => {
    const onChange = vi.fn();
    render(<PasteEditor onChange={onChange} />);
    const element = await screen.findByRole("textbox", { name: "待翻译原文" });
    const clipboard: Record<string, string> = { "text/plain": markdown };
    if (format !== "plain") clipboard["text/html"] = highlightedHtml;
    if (format === "code editor") {
      clipboard["vscode-editor-data"] = JSON.stringify({ version: 1, mode: "markdown" });
    }
    if (format === "markdown MIME") clipboard["text/markdown"] = markdown;
    await pasteClipboardData(element, clipboard);

    expect(within(element).getByRole("heading", { level: 2, name: "First run" })).toBeTruthy();
    expect(within(element).getAllByRole("listitem")).toHaveLength(2);
    expect(within(element).getByText("Microsoft Translator").tagName).toBe("STRONG");
    const exported = onChange.mock.lastCall?.[0] as string;
    expect(exported).toContain("1. **Microsoft Translator**");
    expect(exported).not.toContain("1\\.");
    expect(exported).not.toContain("\\*");
    expect(exported).not.toContain("&#xA;");
  });

  it("replaces only the selection and keeps paste undoable without remounting", async () => {
    const onChange = vi.fn();
    render(<PasteEditor initialValue="Before remove after." onChange={onChange} />);
    const element = await screen.findByRole("textbox", { name: "待翻译原文" });
    const editor = await pasteClipboardData(element, { "text/plain": "**replacement**" }, () => {
      const text = $getRoot().getFirstDescendant();
      if (!$isTextNode(text)) throw new Error("Expected the initial paragraph text");
      text.select(7, 13);
    });
    expect(onChange).toHaveBeenLastCalledWith("Before **replacement** after.");
    await act(async () => { editor.dispatchCommand(UNDO_COMMAND, undefined); });
    expect(onChange).toHaveBeenLastCalledWith("Before remove after.");
    await act(async () => { editor.dispatchCommand(REDO_COMMAND, undefined); });
    expect(onChange).toHaveBeenLastCalledWith("Before **replacement** after.");
    expect(screen.getByRole("textbox", { name: "待翻译原文" })).toBe(element);
  });

  it("keeps intentional escapes and code literal across a source-mode round trip", async () => {
    const literalMarkdown = "1\\. Literal marker\n\n\\*\\*Literal stars\\*\\*\n\n```text\n1. code\n**not bold**\n```";
    render(<PasteEditor />);
    const element = await screen.findByRole("textbox", { name: "待翻译原文" });
    await pasteClipboardData(element, { "text/plain": literalMarkdown });
    expect(within(element).queryByRole("list")).toBeNull();
    expect(element.querySelector("strong")).toBeNull();
    expect(within(element).getByText("1. Literal marker")).toBeTruthy();
    expect((screen.getByRole("textbox", { name: "代码块：text" }) as HTMLTextAreaElement).value)
      .toBe("1. code\n**not bold**");
    fireEvent.click(screen.getByRole("button", { name: "原文 Markdown 源码" }));
    expect((screen.getByRole("textbox") as HTMLTextAreaElement).value).toBe(literalMarkdown);
  });

  it("preserves semantic HTML pastes instead of reinterpreting their literal numbers", async () => {
    const onChange = vi.fn();
    render(<PasteEditor onChange={onChange} />);
    const element = await screen.findByRole("textbox", { name: "待翻译原文" });
    await pasteClipboardData(element, {
      "text/plain": "1. Literal text\nActual list item",
      "text/html": "<p>1. Literal text</p><ul><li><strong>Actual list item</strong></li></ul>",
    });
    expect(element.querySelector("ol")).toBeNull();
    expect(within(element).getAllByRole("listitem")).toHaveLength(1);
    expect(within(element).getByText("Actual list item").tagName).toBe("STRONG");
    expect(onChange.mock.lastCall?.[0]).toContain("1\\. Literal text");
  });

  it("preserves unsupported pasted markup entirely without partial import or remote elements", async () => {
    const unsupportedMarkdown = "# Heading\n\n![Private](https://example.invalid/pixel)\n\n<script>alert(1)</script>";
    const { container } = render(<PasteEditor initialValue="Existing text." />);
    const element = await screen.findByRole("textbox", { name: "待翻译原文" });
    const editor = await pasteClipboardData(element, { "text/markdown": unsupportedMarkdown });
    expect(editor.getEditorState().read(() => $getRoot().getTextContent()))
      .toBe(`Existing text.${unsupportedMarkdown}`);
    expect(element.querySelector("h1")).toBeNull();
    expect(container.querySelector("img, script, iframe")).toBeNull();
  });

  it("imports nested lists, CJK emphasis and tables with the configured Markdown dialect", async () => {
    render(<PasteEditor />);
    const element = await screen.findByRole("textbox", { name: "待翻译原文" });
    await pasteClipboardData(element, {
      "text/plain": "1. **注意：**保留列表\n   - Nested item\n2. Next\n\n| Name | Value |\n| --- | --- |\n| Entry | Detail |",
    });
    expect(within(element).getByText("注意：").tagName).toBe("STRONG");
    expect(element.querySelector("ol ul")).not.toBeNull();
    expect(within(element).getByRole("table")).toBeTruthy();
    expect(within(element).getByText("Detail")).toBeTruthy();
  });
});
