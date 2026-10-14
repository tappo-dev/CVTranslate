import { useRef, useState } from "react";
import { Copy, Eye, FileText, Plus, Save, Star, Trash2 } from "lucide-react";
import { request } from "./api";
import { useFeedback } from "./feedback";
import type { UpdateState } from "./App";
import type { AppState, Prompt, Settings } from "./types";
import { Empty, Notice, useAction } from "./ui";
import { validatePersona, validateTemplates } from "./validation";
import { LanguageOptions } from "./LanguageOptions";

type TemplateField = "systemTemplate" | "userTemplate";
const seriousDeveloperPersona = `# Serious Developer
Express the translation in the voice of a serious, reserved developer who is a native English speaker.
Use precise, emotionally neutral wording. Keep the communication focused on solving the problem, without adding emotional color.
When translating into English, use natural phrasing and common words. Avoid obscure words when everyday words convey the meaning clearly. Use technical terms when needed for accuracy.
Preserve the original meaning, facts, requests, and constraints. Always use the selected target language.`;

export function PromptsPage({
  state,
  updateState,
}: {
  state: AppState;
  updateState: UpdateState;
}) {
  const [draft, setDraft] = useState<Prompt | null>(
    state.prompts[0] ? { ...state.prompts[0] } : null,
  );
  const [previewText, setPreviewText] = useState("");
  const [sourceLanguage, setSourceLanguage] = useState(
    state.settings.sourceLanguage,
  );
  const [targetLanguage, setTargetLanguage] = useState(
    state.settings.targetLanguage === "auto"
      ? "en"
      : state.settings.targetLanguage,
  );
  const [preview, setPreview] = useState<{
    system: string;
    user: string;
  } | null>(null);
  const systemInput = useRef<HTMLTextAreaElement>(null);
  const userInput = useRef<HTMLTextAreaElement>(null);
  const action = useAction();
  const { confirm } = useFeedback();
  const validation = draft
    ? validateTemplates(draft.systemTemplate, draft.userTemplate) ||
      validatePersona(draft.persona)
    : "";
  function edit(prompt: Prompt) {
    setDraft({ ...prompt });
    setPreview(null);
    action.setError("");
  }
  function changeField(field: TemplateField | "persona", value: string) {
    if (draft) setDraft({ ...draft, [field]: value });
    setPreview(null);
  }
  function insertVariable(field: TemplateField, variable: string) {
    if (!draft || draft.builtIn) return;
    const input =
      field === "systemTemplate" ? systemInput.current : userInput.current;
    if (!input) return;
    const token = `{{${variable}}}`;
    const position = input.selectionStart;
    changeField(
      field,
      draft[field].slice(0, position) +
        token +
        draft[field].slice(input.selectionEnd),
    );
    requestAnimationFrame(() => {
      if (input.isConnected) {
        input.focus();
        input.setSelectionRange(
          position + token.length,
          position + token.length,
        );
      }
    });
  }
  function save() {
    if (!draft || draft.builtIn || validation) return;
    if (
      !draft.name.trim() ||
      new TextEncoder().encode(draft.name.trim()).length > 160
    ) {
      action.setError("模板名称不能为空，且不能超过 160 字节。");
      return;
    }
    void action.run(async (signal) => {
      const saved = await request<Prompt>("/prompts", {
        method: "POST",
        body: draft,
        signal,
      });
      if (!signal.aborted) {
        updateState({
          ...state,
          prompts: [
            ...state.prompts.filter((prompt) => prompt.id !== saved.id),
            saved,
          ],
        });
        edit(saved);
      }
    }, "模板已保存");
  }
  return (
    <>
      <div className="section-heading">
        <button
          className="primary"
          disabled={action.busy}
          onClick={() =>
            edit({
              id: "",
              name: "",
              systemTemplate:
                "Translate to {{target_language}}. Return only the translation.",
              userTemplate: "{{text}}",
              persona: "",
              revision: 0,
              builtIn: false,
            })
          }
        >
          <Plus size={17} />
          新建模板
        </button>
      </div>
      <Notice error>{action.error}</Notice>
      <div className="prompt-layout">
        <section className="item-list">
          {state.prompts.map((prompt) => (
            <article
              className={`card prompt-card ${draft?.id === prompt.id ? "selected-card" : ""}`}
              key={prompt.id}
            >
              <div className="section-heading">
                <FileText size={21} />
                <span className="badge">r{prompt.revision}</span>
              </div>
              <h3>{prompt.name}</h3>
              <div className="badges">
                {prompt.builtIn && <span className="badge">内置 · 只读</span>}
                {prompt.persona.trim() && <span className="badge">含人设</span>}
                {state.settings.defaultPromptId === prompt.id && (
                  <span className="badge succeeded">默认模板</span>
                )}
              </div>
              <p className="template-excerpt">{prompt.systemTemplate}</p>
              <div className="button-row wrap">
                <button disabled={action.busy} onClick={() => edit(prompt)}>
                  {prompt.builtIn ? "查看" : "编辑"}
                </button>
                <button
                  disabled={action.busy}
                  onClick={() =>
                    edit({
                      ...prompt,
                      id: "",
                      name: `${prompt.name} 副本`,
                      revision: 0,
                      builtIn: false,
                    })
                  }
                >
                  <Copy size={14} />
                  创建副本
                </button>
                <button
                  className="icon-button"
                  title="设为默认"
                  aria-label={`将 ${prompt.name} 设为默认`}
                  disabled={
                    action.busy || prompt.id === state.settings.defaultPromptId
                  }
                  onClick={() => {
                    void action.run(async (signal) => {
                      const settings = await request<Settings>("/settings", {
                        method: "PUT",
                        body: { ...state.settings, defaultPromptId: prompt.id },
                        signal,
                      });
                      if (!signal.aborted) updateState({ ...state, settings });
                    }, "默认模板已更新。");
                  }}
                >
                  <Star size={15} />
                </button>
                <button
                  className="icon-button danger"
                  aria-label={`删除 ${prompt.name}`}
                  disabled={
                    action.busy ||
                    prompt.builtIn ||
                    prompt.id === state.settings.defaultPromptId
                  }
                  onClick={async () => {
                    if (
                      !(await confirm({
                        title: `删除模板“${prompt.name}”？`,
                        description: "无法撤销。历史快照不受影响。",
                        confirmLabel: "删除",
                        danger: true,
                      }))
                    )
                      return;
                    void action.run(async (signal) => {
                      await request(
                        `/prompts/${encodeURIComponent(prompt.id)}`,
                        { method: "DELETE", signal },
                      );
                      if (!signal.aborted) {
                        updateState({
                          ...state,
                          prompts: state.prompts.filter(
                            (item) => item.id !== prompt.id,
                          ),
                        });
                        if (draft?.id === prompt.id) setDraft(null);
                      }
                    }, "模板已删除。");
                  }}
                >
                  <Trash2 size={15} />
                </button>
              </div>
            </article>
          ))}
        </section>
        {draft ? (
          <section className="card editor-card">
            <div className="section-heading">
              <div>
                <h2>
                  {draft.id
                    ? draft.builtIn
                      ? "内置模板"
                      : "编辑模板"
                    : "新建模板"}
                </h2>
                {draft.id && <p>r{draft.revision}</p>}
              </div>
              {draft.builtIn && (
                <button
                  disabled={action.busy}
                  onClick={() =>
                    edit({
                      ...draft,
                      id: "",
                      name: `${draft.name} 副本`,
                      revision: 0,
                      builtIn: false,
                    })
                  }
                >
                  <Copy size={15} />
                  复制并编辑
                </button>
              )}
            </div>
            <form
              onSubmit={(event) => {
                event.preventDefault();
                save();
              }}
            >
              <fieldset disabled={action.busy}>
                <label>
                  模板名称
                  <input
                    required
                    readOnly={draft.builtIn}
                    value={draft.name}
                    onChange={(event) =>
                      setDraft({ ...draft, name: event.target.value })
                    }
                  />
                </label>
                {(["systemTemplate", "userTemplate"] as const).map((field) => (
                  <div className="template-field" key={field}>
                    <label>
                      {field === "systemTemplate" ? "系统模板" : "用户模板"}
                      <textarea
                        ref={
                          field === "systemTemplate" ? systemInput : userInput
                        }
                        className="code-input"
                        readOnly={draft.builtIn}
                        rows={field === "systemTemplate" ? 5 : 4}
                        value={draft[field]}
                        onChange={(event) =>
                          changeField(field, event.target.value)
                        }
                      />
                    </label>
                    <div className="variable-buttons">
                      <span>插入变量</span>
                      {(field === "systemTemplate"
                        ? ["source_language", "target_language"]
                        : ["source_language", "target_language", "text"]
                      ).map((variable) => (
                        <button
                          type="button"
                          key={variable}
                          disabled={draft.builtIn}
                          onMouseDown={(event) => event.preventDefault()}
                          onClick={() => insertVariable(field, variable)}
                        >{`{{${variable}}}`}</button>
                      ))}
                    </div>
                  </div>
                ))}
                <div className="template-field">
                  <label>
                    人设（可选）
                    <textarea
                      rows={7}
                      readOnly={draft.builtIn}
                      value={draft.persona}
                      placeholder="性格、语气和表达方式"
                      aria-describedby="persona-help"
                      onChange={(event) =>
                        changeField("persona", event.target.value)
                      }
                    />
                  </label>
                  <div className="button-row wrap">
                    <button
                      type="button"
                      disabled={draft.builtIn}
                      onClick={async () => {
                        if (
                          draft.persona.trim() &&
                          draft.persona !== seriousDeveloperPersona &&
                          !(await confirm({
                            title: "替换人设？",
                            description: "用“严肃开发者”示例替换当前草稿。",
                            confirmLabel: "替换",
                          }))
                        )
                          return;
                        changeField("persona", seriousDeveloperPersona);
                      }}
                    >
                      填入“严肃开发者”示例
                    </button>
                    <button
                      type="button"
                      disabled={draft.builtIn || !draft.persona}
                      onClick={() => changeField("persona", "")}
                    >
                      清除人设
                    </button>
                    <span className="character-count">
                      {Array.from(draft.persona).length} / 4,000 字符
                    </span>
                  </div>
                  <p className="help-text" id="persona-help">
                    仅用于支持提示词的服务。
                  </p>
                </div>
                <Notice error>{validation}</Notice>
                {!draft.builtIn && (
                  <button
                    className="primary"
                    disabled={!!validation}
                    type="submit"
                  >
                    <Save size={16} />
                    {action.busy ? "正在保存…" : "保存模板"}
                  </button>
                )}
              </fieldset>
            </form>
            <div className="preview-section">
              <h3>
                渲染预览 <span className="badge">本地 · 不计费</span>
              </h3>
              <fieldset disabled={action.busy}>
                <div className="form-grid">
                  <label>
                    源语言
                    <select
                      value={sourceLanguage}
                      onChange={(event) => {
                        setSourceLanguage(event.target.value);
                        setPreview(null);
                      }}
                    >
                      <LanguageOptions state={state} source />
                    </select>
                  </label>
                  <label>
                    目标语言
                    <select
                      value={targetLanguage}
                      onChange={(event) => {
                        setTargetLanguage(event.target.value);
                        setPreview(null);
                      }}
                    >
                      <LanguageOptions state={state} />
                    </select>
                  </label>
                </div>
                <label>
                  预览文本
                  <textarea
                    rows={3}
                    value={previewText}
                    placeholder="输入用于预览的文本，不会调用模型"
                    onChange={(event) => {
                      setPreviewText(event.target.value);
                      setPreview(null);
                    }}
                  />
                </label>
                <button
                  disabled={
                    !!validation ||
                    !previewText.trim() ||
                    Array.from(previewText).length > 20000
                  }
                  onClick={() => {
                    void action.run(async (signal) => {
                      const result = await request<{
                        system: string;
                        user: string;
                      }>("/prompts/preview", {
                        method: "POST",
                        body: {
                          systemTemplate: draft.systemTemplate,
                          userTemplate: draft.userTemplate,
                          persona: draft.persona,
                          text: previewText,
                          sourceLanguage,
                          targetLanguage,
                        },
                        signal,
                      });
                      if (!signal.aborted) setPreview(result);
                    });
                  }}
                >
                  <Eye size={16} />
                  预览渲染结果
                </button>
              </fieldset>
              {preview && (
                <div className="preview-result">
                  <h4>System</h4>
                  <pre>{preview.system}</pre>
                  <h4>User</h4>
                  <pre>{preview.user}</pre>
                </div>
              )}
            </div>
          </section>
        ) : (
          <div className="card editor-placeholder">
            <Empty title="未选择模板" />
          </div>
        )}
      </div>
    </>
  );
}
