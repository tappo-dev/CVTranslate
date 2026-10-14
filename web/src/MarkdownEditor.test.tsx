// @vitest-environment jsdom
import { readFileSync } from "node:fs";
import { join } from "node:path";
import { useState } from "react";
import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
  within,
} from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import {
  MarkdownPreview,
  SourceMarkdownEditor,
  type SourceMarkdownEditorProps,
} from "./MarkdownEditor";

afterEach(cleanup);

function ControlledSourceEditor(props: SourceMarkdownEditorProps) {
  const [mode, setMode] = useState(props.mode);
  return (
    <SourceMarkdownEditor
      {...props}
      mode={mode}
      onModeChange={(updatedMode) => {
        setMode(updatedMode);
        props.onModeChange(updatedMode);
      }}
    />
  );
}

function installWorkspaceStyles() {
  const stylesheet = document.createElement("style");
  // JSDOM's font shorthand masks later size/line-height overrides unless expanded.
  const inheritedFont = [
    "font-family", "font-size", "font-style", "font-weight",
    "font-variant", "font-stretch", "line-height",
  ].map((property) => `${property}: inherit;`).join(" ");
  stylesheet.textContent = ["styles.css", "markdown-editor.css"]
    .map((filename) => readFileSync(join(process.cwd(), "src", filename), "utf8"))
    .join("\n")
    .replaceAll("font: inherit;", inheritedFont);
  document.head.append(stylesheet);
  return stylesheet;
}

describe("Markdown editor", () => {
  const chineseMarkdown = [
    "果：**。",
    "",
    "怎么解决",
    "**现在可用的方法：**复制实际的 Markdown 源码，再粘贴到翻译页的 </> 源码模式。",
    "**要让你现在这种操作直接保留格式：**需要补上“读取剪贴板 HTML → 安全转换为 Markdown → 提交翻译”的链路；没有富文本时仍保留原始纯文本，而不是让前端猜格式。",
  ].join("\n");
  const editorProps: SourceMarkdownEditorProps = {
    value: "# Heading\n\n**Important** text",
    mode: "markdown",
    onModeChange: vi.fn(),
    disabled: false,
    invalid: false,
    onChange: vi.fn(),
    onBlur: vi.fn(),
    onCompositionChange: vi.fn(),
    onSubmit: vi.fn(),
  };

  it("renders editable Markdown with only the header mode switch and no toolbar", async () => {
    const onChange = vi.fn();
    render(<ControlledSourceEditor {...editorProps} onChange={onChange} />);
    const editor = await screen.findByRole("textbox", { name: "待翻译原文" });
    expect(editor.getAttribute("contenteditable")).toBe("true");
    expect(within(editor).getByRole("heading", { name: "Heading" })).toBeTruthy();
    expect(within(editor).getByText("Important").tagName).toBe("STRONG");
    expect(screen.queryByRole("toolbar")).toBeNull();
    expect(screen.queryByRole("combobox")).toBeNull();
    const modeSwitch = screen.getByRole("group", { name: "原文显示模式" });
    expect(modeSwitch.closest(".pane-heading")).not.toBeNull();
    expect(within(modeSwitch).getAllByRole("button")).toHaveLength(2);
    expect(screen.getByRole("button", { name: "原文 Markdown 排版" })
      .getAttribute("aria-pressed")).toBe("true");
    expect(screen.getByRole("button", { name: "原文 Markdown 源码" })
      .getAttribute("aria-pressed")).toBe("false");
    expect(onChange).not.toHaveBeenCalled();
  });

  it("renders copied Chinese emphasis by default without rewriting the source", async () => {
    const onChange = vi.fn();
    render(<ControlledSourceEditor {...editorProps} value={chineseMarkdown} onChange={onChange} />);
    const editor = await screen.findByRole("textbox", { name: "待翻译原文" });
    expect(within(editor).getByText("现在可用的方法：").tagName).toBe("STRONG");
    expect(within(editor).getByText("要让你现在这种操作直接保留格式：").tagName).toBe("STRONG");
    expect(editor.textContent).toContain("果：**。");
    fireEvent.click(screen.getByRole("button", { name: "原文 Markdown 源码" }));
    expect((screen.getByRole("textbox") as HTMLTextAreaElement).value).toBe(chineseMarkdown);
    fireEvent.click(screen.getByRole("button", { name: "原文 Markdown 排版" }));
    expect((await screen.findByText("现在可用的方法：")).tagName).toBe("STRONG");
    expect(onChange).not.toHaveBeenCalled();
  });

  it("uses the same Chinese emphasis rules in the translation preview", () => {
    render(<MarkdownPreview text={chineseMarkdown} />);
    expect(screen.getByText("现在可用的方法：").tagName).toBe("STRONG");
    expect(screen.getByText("要让你现在这种操作直接保留格式：").tagName).toBe("STRONG");
    expect(screen.getByText("果：**。")).toBeTruthy();
  });

  it.each(["source", "translation"])("keeps %s Markdown compact without shrinking text or losing line breaks", async (pane) => {
    const stylesheet = installWorkspaceStyles();
    const markdown = "# Heading\n\nFirst line\nSecond line\n\n> Quoted line\n\n- List item\n\n```text\n  indented\nnext line\n```";
    try {
      const { container } = render(pane === "source"
        ? <ControlledSourceEditor {...editorProps} value={markdown} />
        : <div className="translation-output"><MarkdownPreview text={markdown} /></div>);
      await screen.findByRole("heading", { name: "Heading" });
      const prose = container.querySelector<HTMLElement>(".markdown-prose")!;
      const paragraph = within(prose).getByText("First line Second line").closest("p")!;
      expect(getComputedStyle(prose).fontSize).toBe("16px");
      expect(getComputedStyle(prose).lineHeight).toBe("1.5");
      expect(getComputedStyle(paragraph).marginBlock).toBe("0 var(--space-control)");
      if (pane === "translation") {
        // Collapse renderer separators, not the newlines inside user text.
        expect(getComputedStyle(prose).whiteSpace).toBe("normal");
        expect(getComputedStyle(paragraph).whiteSpace).toBe("pre-wrap");
        expect(paragraph.textContent).toBe("First line\nSecond line");
        for (const block of prose.querySelectorAll("blockquote, ul")) {
          expect(getComputedStyle(block).whiteSpace).toBe("normal");
        }
        const codeBlock = prose.querySelector("pre")!;
        expect(getComputedStyle(codeBlock).whiteSpace).toBe("pre");
        expect(getComputedStyle(codeBlock).lineHeight).toBe("1.5");
        expect(codeBlock.textContent).toBe("  indented\nnext line\n");
      } else {
        const codeEditor = screen.getByRole("textbox", { name: "代码块：text" });
        expect(getComputedStyle(codeEditor).lineHeight).toBe("1.5");
        fireEvent.click(screen.getByRole("button", { name: "原文 Markdown 源码" }));
        expect((screen.getByRole("textbox") as HTMLTextAreaElement).value).toBe(markdown);
      }
    } finally {
      stylesheet.remove();
    }
  });

  it("uses compact source line heights while preserving blank lines and indentation", () => {
    const stylesheet = installWorkspaceStyles();
    const markdown = "# Heading\n\n\n  indented\nnext line\n";
    try {
      const { container } = render(<>
        <ControlledSourceEditor {...editorProps} mode="source" value={markdown} />
        <div className="translation-output"><p className="preserve-text">{markdown}</p></div>
      </>);
      const source = screen.getByRole("textbox") as HTMLTextAreaElement;
      const translation = container.querySelector<HTMLElement>(".preserve-text")!;
      expect(getComputedStyle(source).lineHeight).toBe("1.5");
      expect(getComputedStyle(translation).lineHeight).toBe("1.5");
      expect(getComputedStyle(source).fontSize).toBe("14px");
      expect(getComputedStyle(translation.parentElement!).fontSize).toBe("16px");
      expect(getComputedStyle(translation).whiteSpace).toBe("pre-wrap");
      expect(source.value).toBe(markdown);
      expect(translation.textContent).toBe(markdown);
    } finally {
      stylesheet.remove();
    }
  });

  it.each([
    { density: "regular", proseSize: "16px", codeSize: "14px", letterSpacing: "normal" },
    { density: "compact", proseSize: "14px", codeSize: "13px", letterSpacing: "-0.1px" },
    { density: "dense", proseSize: "12px", codeSize: "12px", letterSpacing: "-0.15px" },
  ])("applies $density typography to both panes without shrinking controls or code spacing", async ({ density, proseSize, codeSize, letterSpacing }) => {
    const stylesheet = installWorkspaceStyles();
    const markdown = "# Heading\n\n## Section\n\n### Subsection\n\n#### Detail\n\nParagraph with `code`.\n\n```text\n  indented\nnext line\n```";
    try {
      const { container } = render(
        <div className="editor-columns" data-text-density={density}>
          <ControlledSourceEditor {...editorProps} value={markdown} />
          <div className="translation-output">
            <MarkdownPreview text={markdown} />
            <p className="preserve-text">{markdown}</p>
          </div>
        </div>,
      );
      await screen.findByRole("textbox", { name: "代码块：text" });
      const proseRegions = container.querySelectorAll(".markdown-prose");
      expect(proseRegions).toHaveLength(2);
      for (const prose of proseRegions) {
        expect(getComputedStyle(prose).fontSize).toBe(proseSize);
        expect(getComputedStyle(prose).lineHeight).toBe("1.5");
        expect(getComputedStyle(prose).letterSpacing).toBe(letterSpacing);
        const headingSizes = [...prose.querySelectorAll("h1, h2, h3, h4")]
          .map((heading) => Number.parseFloat(getComputedStyle(heading).fontSize));
        expect(headingSizes).toHaveLength(4);
        expect(Math.min(...headingSizes)).toBeGreaterThanOrEqual(12);
        expect(headingSizes).toEqual([...headingSizes].sort((first, second) => second - first));
      }
      fireEvent.click(screen.getByRole("button", { name: "原文 Markdown 源码" }));
      for (const code of container.querySelectorAll("code, pre, .markdown-source-editor, .preserve-text")) {
        expect(getComputedStyle(code).fontSize).toBe(codeSize);
        expect(getComputedStyle(code).lineHeight).toBe("1.5");
        expect(getComputedStyle(code).letterSpacing).toBe("normal");
      }
      const modeButton = screen.getByRole("button", { name: "原文 Markdown 源码" });
      expect(getComputedStyle(modeButton).width).toBe("32px");
      expect(getComputedStyle(modeButton).minHeight).toBe("32px");
      expect((screen.getByRole("textbox") as HTMLTextAreaElement).value).toBe(markdown);
      expect(container.querySelector(".preserve-text")!.textContent).toBe(markdown);
    } finally {
      stylesheet.remove();
    }
  });

  it("lets every actual editor wrapper stretch to fill the pane in both modes", async () => {
    const stylesheet = document.createElement("style");
    stylesheet.textContent = readFileSync(join(process.cwd(), "src/markdown-editor.css"), "utf8");
    document.head.append(stylesheet);
    try {
      const { container } = render(<ControlledSourceEditor {...editorProps} value="" />);
      const editor = await screen.findByRole("textbox", { name: "待翻译原文" });
      const fillLayers = container.querySelectorAll(
        ".source-markdown-editor, .markdown-editor-body, .mdxeditor, .mdxeditor-root-contenteditable, .mdxeditor-contenteditable-wrapper",
      );
      expect(fillLayers).toHaveLength(5);
      // These are CSS constraints, not a substitute for browser geometry checks.
      for (const layer of [...fillLayers, editor]) {
        expect(getComputedStyle(layer).flexGrow, layer.className).toBe("1");
        expect(getComputedStyle(layer).flexBasis, layer.className).toBe("auto");
      }
      expect(container.querySelector(".markdown-editor-body")?.tagName).toBe("DIV");
      expect(getComputedStyle(editor).width).toBe("100%");
      expect(getComputedStyle(editor).overflowY).toBe("auto");
      const minimumContentHeight = getComputedStyle(editor).minHeight;
      expect(minimumContentHeight).toBe(
        "var(--translation-content-min-height, 325px)",
      );
      fireEvent.click(screen.getByRole("button", { name: "原文 Markdown 源码" }));
      const source = screen.getByRole("textbox", { name: "待翻译原文" });
      expect(getComputedStyle(source).flexGrow).toBe("1");
      expect(getComputedStyle(source).width).toBe("100%");
      expect(getComputedStyle(source).overflowY).toBe("auto");
      expect(getComputedStyle(source).minHeight).toBe(
        minimumContentHeight,
      );
      expect(screen.queryByRole("toolbar")).toBeNull();
    } finally {
      stylesheet.remove();
    }
  });

  it("keeps exact source through mode switches and external task loads", async () => {
    const onChange = vi.fn();
    const original = "## Heading\n\n* item\n\n";
    const { rerender } = render(
      <ControlledSourceEditor {...editorProps} value={original} onChange={onChange} />,
    );
    await screen.findByRole("heading", { name: "Heading" });
    const sourceButton = screen.getByRole("button", { name: "原文 Markdown 源码" });
    const markdownButton = screen.getByRole("button", { name: "原文 Markdown 排版" });
    fireEvent.click(sourceButton);
    expect(sourceButton.getAttribute("aria-pressed")).toBe("true");
    expect(markdownButton.getAttribute("aria-pressed")).toBe("false");
    expect((screen.getByRole("textbox") as HTMLTextAreaElement).value).toBe(original);
    fireEvent.click(markdownButton);
    expect(markdownButton.getAttribute("aria-pressed")).toBe("true");
    await screen.findByRole("heading", { name: "Heading" });
    rerender(<ControlledSourceEditor {...editorProps} value="# New task" onChange={onChange} />);
    await screen.findByRole("heading", { name: "New task" });
    expect(screen.queryByRole("heading", { name: "Heading" })).toBeNull();
    expect(onChange).not.toHaveBeenCalled();
  });

  it("preserves Chinese emphasis when exporting edits without remounting the editor", async () => {
    const onChange = vi.fn();
    const emphasizedParagraph = "**注意：**保留中文加粗。";
    function Example() {
      const [value, setValue] = useState(`${emphasizedParagraph}\n\n\`\`\`js\noriginal\n\`\`\``);
      return <ControlledSourceEditor {...editorProps} value={value} onChange={(markdown) => {
        setValue(markdown);
        onChange(markdown);
      }} />;
    }
    render(<Example />);
    const code = await screen.findByRole("textbox", { name: "代码块：js" });
    await act(async () => fireEvent.change(code, { target: { value: "edited" } }));
    const expectedMarkdown = `${emphasizedParagraph}\n\n\`\`\`js\nedited\n\`\`\``;
    expect(onChange).toHaveBeenLastCalledWith(expectedMarkdown);
    expect(screen.getByRole("textbox", { name: "代码块：js" })).toBe(code);
    fireEvent.click(screen.getByRole("button", { name: "原文 Markdown 源码" }));
    expect((screen.getByRole("textbox") as HTMLTextAreaElement).value).toBe(expectedMarkdown);
    fireEvent.click(screen.getByRole("button", { name: "原文 Markdown 排版" }));
    expect((await screen.findByText("注意：")).tagName).toBe("STRONG");
    expect(onChange).toHaveBeenCalledOnce();
  });

  it.each(["source", "translation"])("keeps escaped markers and code literal in the %s pane", async (pane) => {
    const markdown = [
      "\\*\\*原样：\\*\\*显示",
      "",
      "`**行内：**代码`",
      "",
      "```text",
      "**块内：**代码",
      "```",
    ].join("\n");
    const { container } = render(pane === "source"
      ? <ControlledSourceEditor {...editorProps} value={markdown} />
      : <MarkdownPreview text={markdown} />);
    expect(await screen.findByText("**原样：**显示")).toBeTruthy();
    expect(screen.getByText("**行内：**代码").closest("code")).not.toBeNull();
    if (pane === "source") {
      expect((screen.getByRole("textbox", { name: "代码块：text" }) as HTMLTextAreaElement).value)
        .toBe("**块内：**代码");
    } else {
      expect(screen.getByText("**块内：**代码").tagName).toBe("CODE");
    }
    expect(container.querySelector("strong, em")).toBeNull();
  });

  it("preserves unsupported source and never creates remote images or executable HTML", async () => {
    const source = '# Safe\n\n![Private](https://example.invalid/track)\n\n<script>alert(1)</script>';
    const onChange = vi.fn();
    const { container } = render(<ControlledSourceEditor {...editorProps} value={source} onChange={onChange} />);
    await screen.findByRole("status");
    expect((screen.getByRole("textbox") as HTMLTextAreaElement).value).toBe(source);
    expect(container.querySelector("img, script, iframe")).toBeNull();
    expect(onChange).not.toHaveBeenCalled();
  });

  it("sanitizes raw HTML and replaces remote images with text", () => {
    const { container } = render(
      <MarkdownPreview
        text={[
          '<script data-testid="unsafe-script">alert(1)</script>',
          "",
          "![Tracking pixel](https://example.invalid/tracker.png)",
        ].join("\n")}
      />,
    );

    expect(container.querySelector("script")).toBeNull();
    expect(container.querySelector("img")).toBeNull();
    expect(screen.getByText("[图片：Tracking pixel]")).toBeTruthy();
  });
});
