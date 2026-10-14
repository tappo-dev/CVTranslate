import {
  activeEditor$,
  codeBlockEditorDescriptors$,
  createRootEditorSubscription$,
  defaultCodeBlockLanguage$,
  directiveDescriptors$,
  importMdastTreeToLexical,
  importVisitors$,
  jsxComponentDescriptors$,
  jsxKindMismatchPolicy$,
  realmPlugin,
} from "@mdxeditor/editor";
import {
  $addUpdateTag,
  $getSelection,
  $isRangeSelection,
  COMMAND_PRIORITY_HIGH,
  PASTE_COMMAND,
  PASTE_TAG,
  type LexicalNode,
} from "lexical";
import remarkCjkFriendly from "remark-cjk-friendly/parseOnly";
import remarkGfm from "remark-gfm";
import remarkParse from "remark-parse";
import { unified } from "unified";

export const markdownRemarkPlugins = [remarkGfm, remarkCjkFriendly];
const markdownParser = unified().use(remarkParse).use(markdownRemarkPlugins);

function readClipboardMarkdown(clipboard: DataTransfer): string | null {
  // Editor-to-editor copies already contain semantic nodes, including literal text.
  if (clipboard.getData("application/x-lexical-editor")) return null;
  const explicitMarkdown = clipboard.getData("text/markdown");
  if (explicitMarkdown) return explicitMarkdown;
  const plainText = clipboard.getData("text/plain");
  if (!plainText) return null;

  const editorMetadata = clipboard.getData("vscode-editor-data");
  if (editorMetadata) {
    try {
      const metadata: unknown = JSON.parse(editorMetadata);
      if (metadata && typeof metadata === "object" && "mode" in metadata) {
        // Cursor/VS Code also supply highlighted HTML; it is not document markup.
        return metadata.mode === "markdown" ? plainText : null;
      }
    } catch {
      // Unknown clipboard metadata must not override the normal rich-text path.
      return null;
    }
  }
  return clipboard.getData("text/html") ? null : plainText;
}

export const markdownPastePlugin = realmPlugin({
  init(realm) {
    realm.pub(createRootEditorSubscription$, (editor) => editor.registerCommand(
      PASTE_COMMAND,
      (event) => {
        if (!editor.isEditable() || realm.getValue(activeEditor$) !== editor) return false;
        if (!("clipboardData" in event) || !event.clipboardData) return false;
        const selection = $getSelection();
        if (!$isRangeSelection(selection)) return false;
        const markdown = readClipboardMarkdown(event.clipboardData);
        if (markdown === null) return false;

        // Parse into detached nodes so unsupported content cannot partially replace
        // the selection. Reuse the third-party parser and MDXEditor's own visitors.
        const nodes: LexicalNode[] = [];
        try {
          importMdastTreeToLexical({
            root: {
              append: (node) => { nodes.push(node); },
              getType: () => selection.getNodes()[0]?.getType() ?? "root",
            },
            mdastRoot: markdownParser.parse(markdown),
            visitors: realm.getValue(importVisitors$),
            jsxComponentDescriptors: realm.getValue(jsxComponentDescriptors$),
            jsxKindMismatchPolicy: realm.getValue(jsxKindMismatchPolicy$),
            directiveDescriptors: realm.getValue(directiveDescriptors$),
            codeBlockEditorDescriptors: realm.getValue(codeBlockEditorDescriptors$),
            defaultCodeBlockLanguage: realm.getValue(defaultCodeBlockLanguage$),
          });
        } catch {
          nodes.length = 0;
        }

        event.preventDefault();
        $addUpdateTag(PASTE_TAG);
        if (nodes.length > 0) {
          selection.insertNodes(nodes);
        } else {
          // Keep unsupported markup literally; never import HTML or remote images.
          selection.insertRawText(markdown);
        }
        return true;
      },
      COMMAND_PRIORITY_HIGH,
    ));
  },
});
