import { useEffect, useState } from "react";
import {
  ArrowUpRight,
  ChevronLeft,
  ChevronRight,
  Filter,
  RefreshCw,
  Search,
  Trash2,
  X,
} from "lucide-react";
import { errorMessage, request } from "./api";
import { useFeedback } from "./feedback";
import {
  isActive,
  statusLabels,
  type Task,
  type TaskPage,
  type TaskStatus,
} from "./types";
import {
  CopyButton,
  CopyIconButton,
  Empty,
  Loading,
  Notice,
  Status,
  useAction,
} from "./ui";

function localDateBoundary(dateValue: string, additionalDays = 0) {
  const [year, month, day] = dateValue.split("-").map(Number);
  return new Date(year, month - 1, day + additionalDays).toISOString();
}

export function HistoryPage({
  openTask,
  initialDateFrom = "",
  initialDateTo = "",
  updateDateRange,
}: {
  openTask: (taskId: string) => void;
  initialDateFrom?: string;
  initialDateTo?: string;
  updateDateRange?: (dateFrom: string, dateTo: string) => void;
}) {
  const [query, setQuery] = useState("");
  const [search, setSearch] = useState("");
  const [status, setStatus] = useState("");
  const [dateFrom, setDateFrom] = useState(initialDateFrom);
  const [dateTo, setDateTo] = useState(initialDateTo);
  const [appliedDateFrom, setAppliedDateFrom] = useState(initialDateFrom);
  const [appliedDateTo, setAppliedDateTo] = useState(initialDateTo);
  const [filterError, setFilterError] = useState("");
  const [page, setPage] = useState(1);
  const [data, setData] = useState<TaskPage | null>(null);
  const [selected, setSelected] = useState<Task | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [revision, setRevision] = useState(0);
  const action = useAction();
  const { confirm } = useFeedback();
  useEffect(() => {
    setDateFrom(initialDateFrom);
    setDateTo(initialDateTo);
    setAppliedDateFrom(initialDateFrom);
    setAppliedDateTo(initialDateTo);
    setFilterError("");
    setPage(1);
  }, [initialDateFrom, initialDateTo]);
  useEffect(() => {
    const controller = new AbortController();
    setLoading(true);
    setError("");
    const parameters = new URLSearchParams({
      page: String(page),
      pageSize: "20",
      q: search,
      status,
    });
    if (appliedDateFrom) {
      parameters.set("createdAfter", localDateBoundary(appliedDateFrom));
    }
    if (appliedDateTo) {
      parameters.set("createdBefore", localDateBoundary(appliedDateTo, 1));
    }
    void request<TaskPage>(`/history?${parameters}`, {
      signal: controller.signal,
    })
      .then((result) => {
        if (!controller.signal.aborted) {
          setData({ ...result, items: result.items || [] });
          setSelected(null);
        }
      })
      .catch((caught) => {
        if (!controller.signal.aborted) setError(errorMessage(caught));
      })
      .finally(() => {
        if (!controller.signal.aborted) setLoading(false);
      });
    return () => controller.abort();
  }, [page, search, status, appliedDateFrom, appliedDateTo, revision]);

  function applyDateFilter() {
    if (dateFrom && dateTo && dateFrom > dateTo) {
      setFilterError("开始日期不能晚于结束日期。");
      return;
    }
    setFilterError("");
    setPage(1);
    setAppliedDateFrom(dateFrom);
    setAppliedDateTo(dateTo);
    updateDateRange?.(dateFrom, dateTo);
  }

  function clearDateFilter() {
    setDateFrom("");
    setDateTo("");
    setAppliedDateFrom("");
    setAppliedDateTo("");
    setFilterError("");
    setPage(1);
    updateDateRange?.("", "");
  }

  async function removeTask(task: Task) {
    if (
      !(await confirm({
        title: "删除这条翻译记录？",
        description: "永久删除，无法撤销。",
        confirmLabel: "删除",
        danger: true,
      }))
    )
      return;
    void action.run(async (signal) => {
      await request(`/history/${encodeURIComponent(task.id)}`, {
        method: "DELETE",
        signal,
      });
      if (!signal.aborted) {
        setSelected(null);
        setPage(1);
        setRevision((value) => value + 1);
      }
    }, "历史记录已删除。");
  }
  return (
    <>
      <div className="toolbar">
        <form
          className="toolbar-field toolbar-search"
          onSubmit={(event) => {
            event.preventDefault();
            setPage(1);
            setSearch(query.trim());
          }}
        >
          <label htmlFor="history-search">搜索内容</label>
          <div className="search-box">
            <Search size={17} />
            <input
              id="history-search"
              aria-label="搜索翻译历史"
              placeholder="搜索原文或译文"
              value={query}
              onChange={(event) => setQuery(event.target.value)}
            />
            <button type="submit">搜索</button>
          </div>
        </form>
        <label className="toolbar-field toolbar-status">
          <span>任务状态</span>
          <select
            aria-label="筛选任务状态"
            value={status}
            onChange={(event) => {
              setStatus(event.target.value);
              setPage(1);
            }}
          >
            <option value="">全部状态</option>
            {Object.entries(statusLabels).map(([value, label]) => (
              <option key={value} value={value}>
                {label}
              </option>
            ))}
          </select>
        </label>
        <form
          className="toolbar-filters"
          aria-label="按日期筛选翻译历史"
          onSubmit={(event) => {
            event.preventDefault();
            applyDateFilter();
          }}
        >
          <label className="toolbar-field">
            <span>开始日期</span>
            <input
              type="date"
              value={dateFrom}
              max={dateTo || undefined}
              onChange={(event) => setDateFrom(event.target.value)}
            />
          </label>
          <label className="toolbar-field">
            <span>结束日期</span>
            <input
              type="date"
              value={dateTo}
              min={dateFrom || undefined}
              onChange={(event) => setDateTo(event.target.value)}
            />
          </label>
          <div className="button-row toolbar-actions">
            <button type="submit">
              <Filter size={16} />
              筛选
            </button>
            <button
              type="button"
              className="subtle"
              disabled={!dateFrom && !dateTo && !appliedDateFrom && !appliedDateTo}
              onClick={clearDateFilter}
            >
              <X size={16} />
              清除日期
            </button>
          </div>
        </form>
        <div className="button-row toolbar-actions">
          <button
            type="button"
            className="toolbar-icon-button"
            aria-label="刷新历史"
            title="刷新历史"
            onClick={() => setRevision((value) => value + 1)}
          >
            <RefreshCw size={16} />
          </button>
        </div>
      </div>
      <Notice error>{filterError || error || action.error}</Notice>
      <section className="card history-card">
        {loading ? (
          <Loading />
        ) : !data?.items.length ? (
          <Empty
            title={
              search || status || appliedDateFrom || appliedDateTo
                ? "没有匹配的记录"
                : "暂无翻译记录"
            }
          />
        ) : (
          <>
            <div className="history-labels">
              <span aria-hidden="true" />
              <span>翻译内容</span>
              <span>服务 / 状态</span>
              <span>时间</span>
            </div>
            {data.items.map((task) => (
              <article
                key={task.id}
                className={`history-row ${selected?.id === task.id ? "selected" : ""}`}
              >
                <div className="history-copy-actions">
                  <CopyIconButton text={task.text} label="复制原文" />
                  <CopyIconButton
                    text={task.translation}
                    label="复制译文"
                  />
                </div>
                <button
                  type="button"
                  className="history-row-main"
                  onClick={() => setSelected(task)}
                >
                  <span className="history-excerpt">
                    <strong>{task.text}</strong>
                    <span>
                      {task.translation ||
                        task.errorMessage ||
                        statusLabels[task.status as TaskStatus]}
                    </span>
                  </span>
                  <span className="history-provider">
                    {task.providerSnapshot.name}
                    <Status task={task} />
                  </span>
                  <time>
                    {new Date(task.createdAt).toLocaleString("zh-CN")}
                  </time>
                </button>
              </article>
            ))}
          </>
        )}
        <div className="pagination">
          <span>共 {data?.total ?? 0} 条记录 · 历史以本地明文保存</span>
          <div className="button-row">
            <button
              aria-label="上一页"
              disabled={page <= 1 || loading}
              onClick={() => setPage(page - 1)}
            >
              <ChevronLeft size={16} />
            </button>
            <span>第 {page} 页</span>
            <button
              aria-label="下一页"
              disabled={!data || page * data.pageSize >= data.total || loading}
              onClick={() => setPage(page + 1)}
            >
              <ChevronRight size={16} />
            </button>
          </div>
        </div>
      </section>
      {selected && (
        <section className="card detail-card">
          <div className="section-heading">
            <div>
              <h2>任务详情</h2>
              <p>{selected.id}</p>
            </div>
            <div className="button-row">
              <button onClick={() => openTask(selected.id)}>
                打开任务 <ArrowUpRight size={16} />
              </button>
              <button
                className="danger"
                disabled={isActive(selected) || action.busy}
                onClick={() => removeTask(selected)}
              >
                <Trash2 size={16} />
                删除
              </button>
            </div>
          </div>
          <div className="metadata">
            <span>
              <b>状态</b>
              <Status task={selected} />
            </span>
            <span>
              <b>语言</b>
              {selected.sourceLanguage} → {selected.targetLanguage}
            </span>
            <span>
              <b>耗时</b>
              {selected.durationMs} ms
            </span>
            {selected.tokenUsage && (
              <span>
                <b>Token</b>
                {selected.tokenUsage.totalTokens.toLocaleString("zh-CN")}（输入
                {selected.tokenUsage.promptTokens.toLocaleString("zh-CN")} / 输出
                {selected.tokenUsage.completionTokens.toLocaleString("zh-CN")}）
              </span>
            )}
            <span>
              <b>入口</b>
              {selected.entry}
            </span>
            <span>
              <b>粘贴状态</b>
              {selected.pasteStatus || "未请求"}（不代表已插入）
            </span>
          </div>
          <Notice error>
            {selected.errorMessage &&
              `${selected.errorCode}: ${selected.errorMessage}`}
          </Notice>
          <div className="detail-columns">
            <div>
              <h3>原文</h3>
              <p className="preserve-text">{selected.text}</p>
              <CopyButton text={selected.text} />
            </div>
            <div>
              <h3>译文</h3>
              <p className="preserve-text">
                {selected.translation || "暂无译文"}
              </p>
              <CopyButton text={selected.translation} />
            </div>
          </div>
          <details>
            <summary>执行时配置快照</summary>
            <dl>
              <dt>服务提供商 / 模型</dt>
              <dd>
                {selected.providerSnapshot.name}
                {selected.providerSnapshot.model
                  ? ` / ${selected.providerSnapshot.model}`
                  : ""}
              </dd>
              <dt>端点</dt>
              <dd>{selected.providerSnapshot.baseUrl || "内置服务地址"}</dd>
              {selected.promptSnapshot.id ? (
                <>
                  <dt>提示词 / 版本</dt>
                  <dd>
                    {selected.promptSnapshot.name} / r
                    {selected.promptSnapshot.revision}
                  </dd>
                  <dt>系统模板</dt>
                  <dd className="preserve-text">
                    {selected.promptSnapshot.systemTemplate}
                  </dd>
                  <dt>用户模板</dt>
                  <dd className="preserve-text">
                    {selected.promptSnapshot.userTemplate}
                  </dd>
                  <dt>人设</dt>
                  <dd className="preserve-text">
                    {selected.promptSnapshot.persona.trim() || "未使用人设"}
                  </dd>
                </>
              ) : (
                <>
                  <dt>提示词</dt>
                  <dd>无</dd>
                </>
              )}
            </dl>
          </details>
        </section>
      )}
    </>
  );
}
