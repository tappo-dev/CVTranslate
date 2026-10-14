import { useState } from "react";
import { Copy, KeyRound, Plus, Save, Star, Trash2, Zap } from "lucide-react";
import { request } from "./api";
import { useFeedback } from "./feedback";
import type { UpdateState } from "./App";
import type { AppState, ProviderConfig, Settings } from "./types";
import { Empty, Notice, useAction } from "./ui";
import { validateProviderEndpoint } from "./validation";

const emptyProvider = (defaultModel = "gpt-6-luna"): ProviderConfig => ({
  id: "",
  name: "",
  type: "openai-compatible",
  baseUrl: "",
  model: defaultModel,
  timeoutSeconds: 60,
  enabled: true,
  hasApiKey: false,
});
export function ProvidersPage({
  state,
  updateState,
}: {
  state: AppState;
  updateState: UpdateState;
}) {
  const [draft, setDraft] = useState<ProviderConfig | null>(null);
  const [apiKey, setApiKey] = useState("");
  const [clearApiKey, setClearApiKey] = useState(false);
  const [originalEndpoint, setOriginalEndpoint] = useState("");
  const action = useAction();
  const { confirm } = useFeedback();
  const capabilities = state.providerTypes.find(
    (entry) => entry.type === draft?.type,
  );
  const endpointValidation = validateProviderEndpoint(draft?.baseUrl ?? "");
  function edit(provider: ProviderConfig) {
    setDraft({ ...provider });
    setOriginalEndpoint(provider.baseUrl);
    setApiKey("");
    setClearApiKey(false);
    action.setError("");
  }
  async function save() {
    if (!draft) return;
    if (!capabilities) {
      action.setError("请选择可用的接口类型。");
      return;
    }
    if (capabilities.configurableEndpoint && endpointValidation.error) {
      action.setError(endpointValidation.error);
      return;
    }
    if (
      !draft.name.trim() ||
      (capabilities.requiresModel && !draft.model.trim())
    ) {
      action.setError("请填写名称及此接口要求的模型。");
      return;
    }
    const endpointChanged =
      capabilities.configurableEndpoint &&
      !!draft.id &&
      originalEndpoint !== draft.baseUrl;
    if (
      endpointChanged &&
      !(await confirm({
        title: "更改服务地址？",
        description: "翻译内容及保留的 API 密钥将发送至新地址。",
        confirmLabel: "信任并保存",
      }))
    )
      return;
    void action.run(async (signal) => {
      const saved = await request<ProviderConfig>("/providers", {
        method: "POST",
        body: {
          ...draft,
          baseUrl: capabilities.configurableEndpoint ? draft.baseUrl : "",
          model: capabilities.requiresModel ? draft.model : "",
          ...(capabilities.supportsApiKey
            ? { ...(apiKey ? { apiKey } : {}), clearApiKey }
            : {}),
          confirmEndpointChange: endpointChanged,
        },
        signal,
      });
      if (!signal.aborted) {
        updateState({
          ...state,
          providers: [
            ...state.providers.filter((provider) => provider.id !== saved.id),
            saved,
          ],
        });
        edit(saved);
      }
    }, "服务配置已保存。");
  }
  function makeDefault(provider: ProviderConfig) {
    void action.run(async (signal) => {
      const settings = await request<Settings>("/settings", {
        method: "PUT",
        body: { ...state.settings, defaultProviderId: provider.id },
        signal,
      });
      if (!signal.aborted) updateState({ ...state, settings });
    }, "默认服务已更新。");
  }
  return (
    <>
      <div className="section-heading">
        <button
          className="primary"
          disabled={action.busy}
          onClick={() =>
            edit(
              emptyProvider(
                state.providerTypes.find(
                  (entry) => entry.type === "openai-compatible",
                )?.defaultModel,
              ),
            )
          }
        >
          <Plus size={17} />
          添加服务
        </button>
      </div>
      <Notice error>{action.error}</Notice>
      <div className="management-layout">
        <section className="item-list">
          {!state.providers.length && (
            <div className="card">
              <Empty title="暂无翻译服务" />
            </div>
          )}
          {state.providers.map((provider) => (
            <article
              className={`card provider-card ${draft?.id === provider.id ? "selected-card" : ""}`}
              key={provider.id}
            >
              <div className="section-heading">
                <span className="service-icon">
                  {provider.type === "microsoft-translate" ? "MT" : "AI"}
                </span>
                <div className="badges">
                  {provider.id === state.settings.defaultProviderId && (
                    <span className="badge succeeded">默认</span>
                  )}
                  <span className="badge">
                    {provider.enabled ? "已启用" : "已停用"}
                  </span>
                </div>
              </div>
              <h3>{provider.name}</h3>
              <p className="model-name">
                {provider.type === "microsoft-translate"
                  ? "Microsoft 翻译"
                  : provider.model}
              </p>
              {provider.baseUrl && (
                <p className="endpoint">{provider.baseUrl}</p>
              )}
              <div className="key-status">
                <KeyRound size={14} />
                {state.providerTypes.find(
                  (entry) => entry.type === provider.type,
                )?.supportsApiKey
                  ? provider.hasApiKey
                    ? "密钥已安全保存"
                    : "未保存密钥"
                  : "无需 API 密钥"}
              </div>
              <div className="button-row wrap">
                <button disabled={action.busy} onClick={() => edit(provider)}>
                  编辑
                </button>
                <button
                  title="复制配置，不复制密钥"
                  disabled={action.busy}
                  onClick={() =>
                    edit({
                      ...provider,
                      id: "",
                      name: `${provider.name} 副本`,
                      hasApiKey: false,
                    })
                  }
                >
                  <Copy size={14} />
                  复制
                </button>
                <button
                  disabled={
                    action.busy ||
                    !provider.enabled ||
                    provider.id === state.settings.defaultProviderId
                  }
                  onClick={() => makeDefault(provider)}
                >
                  <Star size={14} />
                  设为默认
                </button>
              </div>
              <div className="button-row card-secondary-actions">
                <button
                  className="text-button"
                  disabled={action.busy || !provider.enabled}
                  onClick={async () => {
                    if (
                      !(await confirm({
                        title: "测试连接？",
                        description:
                          provider.type === "microsoft-translate"
                            ? "测试文本将发送至 Microsoft。"
                            : "将向此服务发送真实请求，可能产生费用。",
                        confirmLabel: "测试连接",
                      }))
                    )
                      return;
                    void action.run(async (signal) => {
                      await request(
                        `/providers/${encodeURIComponent(provider.id)}/test`,
                        { method: "POST", body: {}, signal },
                      );
                    }, "连接测试成功。");
                  }}
                >
                  <Zap size={14} />
                  {provider.type === "microsoft-translate"
                    ? "测试连接"
                    : "测试连接（可能计费）"}
                </button>
                <button
                  className="icon-button danger"
                  aria-label={`删除 ${provider.name}`}
                  disabled={
                    action.busy ||
                    provider.id === state.settings.defaultProviderId
                  }
                  onClick={async () => {
                    if (
                      !(await confirm({
                        title: `删除服务“${provider.name}”？`,
                        description: "同时删除已保存的密钥，无法撤销。",
                        confirmLabel: "删除",
                        danger: true,
                      }))
                    )
                      return;
                    void action.run(async (signal) => {
                      await request(
                        `/providers/${encodeURIComponent(provider.id)}`,
                        { method: "DELETE", signal },
                      );
                      if (!signal.aborted) {
                        updateState({
                          ...state,
                          providers: state.providers.filter(
                            (item) => item.id !== provider.id,
                          ),
                        });
                        if (draft?.id === provider.id) setDraft(null);
                      }
                    }, "服务已删除。");
                  }}
                >
                  <Trash2 size={15} />
                </button>
              </div>
            </article>
          ))}
        </section>
        {draft ? (
          <form
            className="card editor-card"
            onSubmit={(event) => {
              event.preventDefault();
              save();
            }}
          >
            <div className="section-heading">
              <h2>{draft.id ? "编辑服务" : "添加服务"}</h2>
              <button
                type="button"
                className="text-button"
                disabled={action.busy}
                onClick={() => {
                  setDraft(null);
                  setApiKey("");
                }}
              >
                关闭
              </button>
            </div>
            <fieldset disabled={action.busy}>
              <label>
                服务名称
                <input
                  required
                  maxLength={100}
                  value={draft.name}
                  placeholder="例如：我的翻译服务"
                  onChange={(event) =>
                    setDraft({ ...draft, name: event.target.value })
                  }
                />
              </label>
              <label>
                接口类型
                <select
                  value={draft.type}
                  disabled={!!draft.id}
                  onChange={(event) => {
                    const nextCapabilities = state.providerTypes.find(
                      (entry) => entry.type === event.target.value,
                    );
                    setDraft({
                      ...draft,
                      type: event.target.value,
                      baseUrl: nextCapabilities?.configurableEndpoint
                        ? draft.baseUrl
                        : "",
                      model: nextCapabilities?.requiresModel
                        ? draft.model || nextCapabilities.defaultModel
                        : "",
                      hasApiKey: false,
                    });
                    setApiKey("");
                    setClearApiKey(false);
                  }}
                >
                  {state.providerTypes.map((capabilities) => (
                    <option key={capabilities.type} value={capabilities.type}>
                      {capabilities.name}
                    </option>
                  ))}
                </select>
              </label>
              {capabilities?.configurableEndpoint && (
                <>
                  <label>
                    服务地址（Base URL）
                    <input
                      required
                      type="url"
                      value={draft.baseUrl}
                      placeholder="https://api.example.com/v1"
                      onChange={(event) =>
                        setDraft({ ...draft, baseUrl: event.target.value })
                      }
                    />
                  </label>
                  <Notice>{endpointValidation.warning}</Notice>
                </>
              )}
              {capabilities?.requiresModel && (
                <label>
                  模型名称
                  <input
                    required
                    value={draft.model}
                    placeholder="填写服务支持的模型标识"
                    onChange={(event) =>
                      setDraft({ ...draft, model: event.target.value })
                    }
                  />
                </label>
              )}
              {capabilities?.supportsApiKey && (
                <>
                  <label>
                    API 密钥{" "}
                    <span className="inline-hint">
                      {draft.hasApiKey ? "已保存 · 留空保持不变" : "未保存"}
                    </span>
                    <input
                      type="password"
                      autoComplete="new-password"
                      spellCheck={false}
                      value={apiKey}
                      disabled={clearApiKey}
                      placeholder={
                        draft.hasApiKey
                          ? "输入新密钥以替换"
                          : "输入 API 密钥（如服务需要）"
                      }
                      onChange={(event) => setApiKey(event.target.value)}
                    />
                  </label>
                  {draft.hasApiKey && (
                    <label className="checkbox-label">
                      <input
                        type="checkbox"
                        checked={clearApiKey}
                        onChange={(event) => {
                          setClearApiKey(event.target.checked);
                          setApiKey("");
                        }}
                      />
                      清除已保存的密钥
                    </label>
                  )}
                </>
              )}
              <label>
                请求超时（秒）
                <input
                  required
                  type="number"
                  min={5}
                  max={300}
                  value={draft.timeoutSeconds}
                  onChange={(event) =>
                    setDraft({
                      ...draft,
                      timeoutSeconds: Number(event.target.value),
                    })
                  }
                />
              </label>
              <label className="checkbox-label">
                <input
                  type="checkbox"
                  checked={draft.enabled}
                  disabled={draft.id === state.settings.defaultProviderId}
                  onChange={(event) =>
                    setDraft({ ...draft, enabled: event.target.checked })
                  }
                />
                启用此服务
              </label>
              <button className="primary" type="submit">
                <Save size={16} />
                {action.busy ? "正在保存…" : "保存服务"}
              </button>
            </fieldset>
          </form>
        ) : (
          <div className="card editor-placeholder">
            <Empty title="未选择服务" />
          </div>
        )}
      </div>
    </>
  );
}
