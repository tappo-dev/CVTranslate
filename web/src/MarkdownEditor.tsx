import { useEffect, useId, useImperativeHandle, useMemo, useRef, useState, type Ref } from "react";
import { CodeXml, FileText } from "lucide-react";
import {
  MDXEditor,
  NESTED_EDITOR_UPDATED_COMMAND,
  activeEditor$,
  addSyntaxExtension$,
  addToMarkdownExtension$,
  codeBlockPlugin,
  headingsPlugin,
  linkPlugin,
  listsPlugin,
  markdownShortcutPlugin,
  quotePlugin,
  realmPlugin,
  tablePlugin,
  thematicBreakPlugin,
  useCodeBlockEditorContext,
  type CodeBlockEditorProps,
  type MDXEditorProps,
} from "@mdxeditor/editor";
import { cjkFriendlyToMarkdown } from "mdast-util-to-markdown-cjk-friendly";
import { cjkFriendlyExtension } from "micromark-extension-cjk-friendly";
import ReactMarkdown from "react-markdown";
import { markdownPastePlugin, markdownRemarkPlugins } from "./markdown-paste";
import "@mdxeditor/editor/style.css";
import "./markdown-editor.css";

const editorLabels: Record<string, string> = {
  "contentArea.editableMarkdown": "待翻译原文",
  "table.deleteTable": "删除表格",
  "table.columnMenu": "列操作",
  "table.rowMenu": "行操作",
  "table.textAlignment": "文字对齐",
  "table.alignLeft": "左对齐",
  "table.alignCenter": "居中",
  "table.alignRight": "右对齐",
  "table.insertColumnLeft": "向左插入列",
  "table.insertColumnRight": "向右插入列",
  "table.deleteColumn": "删除列",
  "table.insertRowAbove": "向上插入行",
  "table.insertRowBelow": "向下插入行",
  "table.deleteRow": "删除行",
};

const translateEditorLabel: NonNullable<MDXEditorProps["translation"]> = (
  key,
  fallback,
  interpolations = {},
) => {
  let label = editorLabels[key] ?? fallback;
  for (const [name, value] of Object.entries(interpolations)) {
    label = label.replaceAll(`{{${name}}}`, String(value));
  }
  return label;
};

function CodeBlockEditor({ code, language, focusEmitter }: CodeBlockEditorProps) {
  const { setCode } = useCodeBlockEditorContext();
  const textarea = useRef<HTMLTextAreaElement>(null);
  useEffect(() => {
    focusEmitter.subscribe(() => textarea.current?.focus());
  }, [focusEmitter]);
  return (
    <textarea
      ref={textarea}
      className="markdown-code-editor"
      aria-label={language ? `代码块：${language}` : "代码块"}
      value={code}
      rows={Math.max(3, Math.min(16, code.split("\n").length))}
      spellCheck={false}
      onChange={(event) => setCode(event.target.value)}
    />
  );
}

const cjkEmphasisPlugin = realmPlugin({
  init(realm) {
    // Match the preview's CJK punctuation rules on both import and rich-text export.
    realm.pubIn({
      [addSyntaxExtension$]: cjkFriendlyExtension(),
      [addToMarkdownExtension$]: cjkFriendlyToMarkdown(),
    });
  },
});

const contentPlugins = [
  headingsPlugin(),
  listsPlugin(),
  quotePlugin(),
  thematicBreakPlugin(),
  linkPlugin({ disableAutoLink: true }),
  tablePlugin(),
  codeBlockPlugin({
    codeBlockEditorDescriptors: [
      { priority: 0, match: () => true, Editor: CodeBlockEditor },
    ],
  }),
  cjkEmphasisPlugin(),
  markdownShortcutPlugin(),
  markdownPastePlugin(),
];

const flushNestedEditorPlugin = realmPlugin<{
  registerFlush: (flush: () => void) => void;
}>({
  init(realm, parameters) {
    parameters?.registerFlush(() => {
      // Table cells keep a nested Lexical document until their contents are saved.
      realm.getValue(activeEditor$)?.dispatchCommand(
        NESTED_EDITOR_UPDATED_COMMAND,
        undefined,
      );
    });
  },
});

export type MarkdownMode = "markdown" | "source";

export function MarkdownModeSwitch({
  label,
  mode,
  disabled = false,
  onModeChange,
}: {
  label: string;
  mode: MarkdownMode;
  disabled?: boolean;
  onModeChange: (mode: MarkdownMode) => void;
}) {
  const tooltipId = useId();
  return (
    <div
      className="markdown-mode-switch"
      role="group"
      aria-label={`${label}显示模式`}
      data-translation-control
    >
      {([
        { value: "markdown", title: "Markdown 排版", Icon: FileText },
        { value: "source", title: "Markdown 源码", Icon: CodeXml },
      ] as const).map(({ value, title, Icon }) => (
        <button
          key={value}
          type="button"
          aria-label={`${label} ${title}`}
          aria-pressed={mode === value}
          aria-describedby={`${tooltipId}-${value}`}
          disabled={disabled}
          onClick={() => {
            if (mode !== value) onModeChange(value);
          }}
        >
          <Icon size={16} aria-hidden="true" />
          <span className="markdown-mode-tooltip" id={`${tooltipId}-${value}`} role="tooltip">
            {title}
          </span>
        </button>
      ))}
    </div>
  );
}

export interface SourceMarkdownEditorHandle {
  changeMode: (mode: MarkdownMode) => void;
}

export interface SourceMarkdownEditorProps {
  ref?: Ref<SourceMarkdownEditorHandle>;
  value: string;
  mode: MarkdownMode;
  onModeChange: (mode: MarkdownMode) => void;
  disabled: boolean;
  invalid: boolean;
  onChange: (value: string) => void;
  onBlur: (relatedTarget: EventTarget | null) => void;
  onCompositionChange: (composing: boolean) => void;
  onSubmit: () => void;
}

export function SourceMarkdownEditor({
  ref,
  value,
  mode,
  onModeChange,
  disabled,
  invalid,
  onChange,
  onBlur,
  onCompositionChange,
  onSubmit,
}: SourceMarkdownEditorProps) {
  const [editorDocument, setEditorDocument] = useState({
    markdown: value,
    revision: 0,
  });
  const [parseError, setParseError] = useState(false);
  const sourceMode = mode === "source" || parseError;
  const composing = useRef(false);
  const flushNestedEditor = useRef<(() => void) | null>(null);
  const editorPlugins = useMemo(
    () => [
      ...contentPlugins,
      flushNestedEditorPlugin({
        registerFlush: (flush) => {
          flushNestedEditor.current = flush;
        },
      }),
    ],
    [],
  );
  useEffect(() => {
    if (parseError && mode !== "source") onModeChange("source");
  }, [mode, onModeChange, parseError]);

  function changeMode(updatedMode: MarkdownMode) {
    if (!sourceMode) flushNestedEditor.current?.();
    setParseError(false);
    onModeChange(updatedMode);
  }
  // Either pane must commit nested table edits before switching the shared mode.
  useImperativeHandle(ref, () => ({ changeMode }));

  // External task loads/clears reset undo history; the parent's echo of a local
  // edit must not recreate Lexical or move the caret.
  if (value !== editorDocument.markdown) {
    setEditorDocument({ markdown: value, revision: editorDocument.revision + 1 });
    setParseError(false);
  }

  function updateMarkdown(markdown: string) {
    setEditorDocument((current) => ({ ...current, markdown }));
    onChange(markdown);
  }

  return (
    <div
      className="source-markdown-editor"
      data-translation-control
      role="group"
      aria-label="原文编辑"
      aria-disabled={disabled}
      aria-invalid={invalid}
      onBlur={(event) => {
        if (
          event.relatedTarget instanceof Element &&
          (event.currentTarget.contains(event.relatedTarget) ||
            event.relatedTarget.closest(".workspace-mdx-editor"))
        ) {
          return;
        }
        const relatedTarget = event.relatedTarget;
        if (!sourceMode) flushNestedEditor.current?.();
        queueMicrotask(() => onBlur(relatedTarget));
      }}
      onCompositionStartCapture={() => {
        composing.current = true;
        onCompositionChange(true);
      }}
      onCompositionEndCapture={() => {
        composing.current = false;
        onCompositionChange(false);
      }}
      onKeyDownCapture={(event) => {
        if (
          !disabled &&
          !composing.current &&
          !event.nativeEvent.isComposing &&
          (event.ctrlKey || event.metaKey) &&
          event.key === "Enter"
        ) {
          event.preventDefault();
          event.stopPropagation();
          if (!sourceMode) flushNestedEditor.current?.();
          queueMicrotask(onSubmit);
        }
      }}
    >
      <div className="pane-heading">
        <span>原文</span>
        <MarkdownModeSwitch
          label="原文"
          mode={sourceMode ? "source" : "markdown"}
          disabled={disabled}
          onModeChange={changeMode}
        />
      </div>
      {parseError && (
        <p className="markdown-editor-error" role="status">
          此内容暂不支持排版编辑，已保留源码。
        </p>
      )}
      <div className="markdown-editor-body" inert={disabled}>
        {sourceMode ? (
          <textarea
            className="markdown-source-editor"
            aria-label="待翻译原文"
            aria-invalid={invalid}
            disabled={disabled}
            value={value}
            placeholder="输入或粘贴要翻译的内容…"
            onChange={(event) => updateMarkdown(event.target.value)}
          />
        ) : (
          <MDXEditor
            key={editorDocument.revision}
            markdown={value}
            className="workspace-mdx-editor"
            contentEditableClassName="markdown-prose"
            plugins={editorPlugins}
            readOnly={disabled}
            suppressHtmlProcessing
            trim={false}
            translation={translateEditorLabel}
            placeholder="输入或粘贴要翻译的内容…"
            onChange={(markdown, initialNormalize) => {
              if (!initialNormalize && !disabled) updateMarkdown(markdown);
            }}
            onError={() => {
              // Never replace the source with a partial parse or log its text.
              setParseError(true);
            }}
          />
        )}
      </div>
    </div>
  );
}

export function MarkdownPreview({ text }: { text: string }) {
  return (
    <div className="markdown-prose translation-markdown-preview">
      <ReactMarkdown
        remarkPlugins={markdownRemarkPlugins}
        skipHtml
        components={{
          a: ({ children, href }) => (
            <a href={href} target="_blank" rel="noopener noreferrer">
              {children}
            </a>
          ),
          img: ({ alt }) => (
            <span className="markdown-image-placeholder">
              {alt ? `[图片：${alt}]` : "[图片]"}
            </span>
          ),
        }}
      >
        {text}
      </ReactMarkdown>
    </div>
  );
}
