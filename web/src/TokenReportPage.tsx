import { useEffect, useState } from "react";
import { ChevronLeft, ChevronRight, RefreshCw } from "lucide-react";
import { errorMessage, request } from "./api";
import type { DailyTokenUsage, TokenReport, TokenUsageSummary } from "./types";
import { Loading, Notice } from "./ui";

const dailyPageSize = 10;

function formatTokens(value: number) {
  return value.toLocaleString("zh-CN");
}

function formatDate(value: string) {
  return new Date(`${value}T00:00:00`).toLocaleDateString("zh-CN", {
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
  });
}

function SummaryCard({
  label,
  summary,
}: {
  label: string;
  summary: TokenUsageSummary;
}) {
  return (
    <article className="card token-summary-card">
      <span>{label}</span>
      <strong>{formatTokens(summary.totalTokens)}</strong>
      <small>
        输入 {formatTokens(summary.promptTokens)} · 输出{" "}
        {formatTokens(summary.completionTokens)} · {summary.requestCount} 次
      </small>
    </article>
  );
}

function TokenHeatmap({
  daily,
  openHistory,
}: {
  daily: DailyTokenUsage[];
  openHistory: (date: string) => void;
}) {
  const [inspectedDay, setInspectedDay] = useState<DailyTokenUsage | null>(null);
  const firstWeekday = daily.length
    ? new Date(`${daily[0].date}T00:00:00Z`).getUTCDay()
    : 0;
  const weekCount = Math.max(1, Math.ceil((firstWeekday + daily.length) / 7));
  const maximumDailyTokens = Math.max(0, ...daily.map((day) => day.totalTokens));
  const totalTokens = daily.reduce((total, day) => total + day.totalTokens, 0);
  const recordedDays = daily.filter((day) => day.requestCount > 0).length;
  const months = daily.flatMap((day, dayIndex) => {
    const date = new Date(`${day.date}T00:00:00Z`);
    return dayIndex === 0 || date.getUTCDate() === 1
      ? [{
          date: day.date,
          label: `${date.getUTCMonth() + 1}月`,
          week: Math.floor((firstWeekday + dayIndex) / 7),
        }]
      : [];
  });

  return (
    <section className="card token-heatmap-card" aria-label="每日 Token 热力图">
      <div className="section-heading">
        <h2>过去 365 天</h2>
        <span className="heatmap-total">{formatTokens(totalTokens)} Token</span>
      </div>
      <div
        className="token-heatmap-scroll"
        tabIndex={0}
        role="group"
        aria-label="按周排列的 Token 日历"
      >
        <div
          className="token-heatmap"
          style={{
            gridTemplateColumns: `36px repeat(${weekCount}, minmax(var(--heatmap-cell-size), 1fr))`,
          }}
        >
          {months.map((month, monthIndex) => {
            const nextWeek = months[monthIndex + 1]?.week ?? weekCount;
            // Omit labels for partial months too short to fit without overlap.
            if (nextWeek - month.week < 2) return null;
            return (
              <span
                key={month.date}
                className="heatmap-month"
                style={{ gridColumn: `${month.week + 2} / span 2` }}
              >
                {month.label}
              </span>
            );
          })}
          {["周一", "周三", "周五"].map((label, labelIndex) => (
            <span
              key={label}
              className="heatmap-weekday"
              style={{ gridRow: labelIndex * 2 + 3 }}
            >
              {label}
            </span>
          ))}
          {daily.map((day, dayIndex) => {
            const calendarIndex = firstWeekday + dayIndex;
            const level =
              day.totalTokens === 0 || maximumDailyTokens === 0
                ? 0
                : Math.max(1, Math.ceil((day.totalTokens / maximumDailyTokens) * 4));
            const description = `${formatDate(day.date)}：${formatTokens(day.totalTokens)} Token · ${day.requestCount} 次`;
            const cellProperties = {
              className: `heatmap-cell level-${level}`,
              "aria-label": description,
              title: day.requestCount > 0
                ? `${description}；双击查看翻译历史`
                : description,
              onMouseEnter: () => setInspectedDay(day),
              onMouseLeave: () => setInspectedDay(null),
              onFocus: () => setInspectedDay(day),
              onBlur: () => setInspectedDay(null),
              style: {
                gridColumn: Math.floor(calendarIndex / 7) + 2,
                gridRow: calendarIndex % 7 + 2,
              },
            };
            return day.requestCount > 0 ? (
              <button
                {...cellProperties}
                key={day.date}
                type="button"
                onClick={(event) => {
                  // Keyboard and assistive activation have no mouse click count.
                  if (event.detail === 0) openHistory(day.date);
                }}
                onDoubleClick={() => openHistory(day.date)}
              />
            ) : (
              <span {...cellProperties} key={day.date} role="img" />
            );
          })}
        </div>
      </div>
      <div className="heatmap-footer">
        <span>
          {inspectedDay
            ? `${formatDate(inspectedDay.date)}：${formatTokens(inspectedDay.totalTokens)} Token · ${inspectedDay.requestCount} 次`
            : `${recordedDays} 天有记录`}
        </span>
        <div className="heatmap-legend" aria-label="热力图图例">
          <span>少</span>
          {[0, 1, 2, 3, 4].map((level) => (
            <i
              key={level}
              aria-hidden="true"
              className={`heatmap-cell level-${level}`}
            />
          ))}
          <span>多</span>
        </div>
      </div>
    </section>
  );
}

export function TokenReportPage({
  openHistory,
}: {
  openHistory: (date: string) => void;
}) {
  const [report, setReport] = useState<TokenReport | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [revision, setRevision] = useState(0);
  const [page, setPage] = useState(1);

  useEffect(() => {
    const controller = new AbortController();
    const timezoneOffsetMinutes = -new Date().getTimezoneOffset();
    const parameters = new URLSearchParams({
      days: "365",
      timezoneOffsetMinutes: String(timezoneOffsetMinutes),
    });
    setLoading(true);
    setError("");
    void request<TokenReport>(`/token-report?${parameters}`, {
      signal: controller.signal,
    })
      .then((result) => {
        if (!controller.signal.aborted) {
          setReport({ ...result, daily: result.daily || [] });
          setPage(1);
        }
      })
      .catch((caught) => {
        if (!controller.signal.aborted) setError(errorMessage(caught));
      })
      .finally(() => {
        if (!controller.signal.aborted) setLoading(false);
      });
    return () => controller.abort();
  }, [revision]);

  const recordedDays = (report?.daily ?? [])
    .filter((day) => day.requestCount > 0)
    .sort((left, right) => right.date.localeCompare(left.date));
  const pageCount = Math.max(1, Math.ceil(recordedDays.length / dailyPageSize));
  const currentPage = Math.min(page, pageCount);
  const visibleDays = recordedDays.slice(
    (currentPage - 1) * dailyPageSize,
    currentPage * dailyPageSize,
  );

  return (
    <>
      <div className="section-heading token-report-heading">
        <p>仅统计 AI 大模型响应中实际返回的 token 用量，不换算费用。</p>
        <button
          aria-label="刷新 Token 报表"
          disabled={loading}
          onClick={() => setRevision((value) => value + 1)}
        >
          <RefreshCw size={16} />
          刷新
        </button>
      </div>
      <Notice error>{error}</Notice>
      {loading ? (
        <section className="card">
          <Loading />
        </section>
      ) : report ? (
        <>
          <TokenHeatmap daily={report.daily} openHistory={openHistory} />

          <section className="token-summary-grid" aria-label="Token 汇总">
            <SummaryCard label="今日" summary={report.today} />
            <SummaryCard label="近 7 天" summary={report.last7Days} />
            <SummaryCard label="近一月" summary={report.last30Days} />
          </section>

          <section className="card token-table-card">
            <div className="section-heading">
              <div>
                <h2>每日 Token 明细</h2>
                <p>按当前系统时区统计。</p>
              </div>
            </div>
            <div
              className="token-table"
              id="token-daily-table"
              role="table"
              aria-label="每日 Token 报表"
            >
              <div className="token-table-row token-table-header" role="row">
                <span role="columnheader">日期</span>
                <span role="columnheader">请求数</span>
                <span role="columnheader">输入 Token</span>
                <span role="columnheader">输出 Token</span>
                <span role="columnheader">总 Token</span>
              </div>
              {visibleDays.map((day) => (
                <div className="token-table-row" role="row" key={day.date}>
                  <time role="cell" dateTime={day.date}>
                    {formatDate(day.date)}
                  </time>
                  <span role="cell">{day.requestCount}</span>
                  <span role="cell">{formatTokens(day.promptTokens)}</span>
                  <span role="cell">{formatTokens(day.completionTokens)}</span>
                  <strong role="cell">{formatTokens(day.totalTokens)}</strong>
                </div>
              ))}
              {recordedDays.length === 0 && (
                <div role="row">
                  <div role="cell" aria-colspan={5} className="token-table-empty">
                    暂无 Token 记录
                  </div>
                </div>
              )}
            </div>
            <div
              className="pagination token-pagination"
              role="group"
              aria-label="每日 Token 明细分页"
            >
              <span>共 {recordedDays.length} 天 · 每页 {dailyPageSize} 条</span>
              <div className="button-row">
                <button
                  type="button"
                  aria-label="上一页"
                  title="上一页"
                  aria-controls="token-daily-table"
                  disabled={currentPage <= 1}
                  onClick={() => setPage(currentPage - 1)}
                >
                  <ChevronLeft size={16} />
                </button>
                <span aria-live="polite">
                  第 {currentPage} / {pageCount} 页
                </span>
                <button
                  type="button"
                  aria-label="下一页"
                  title="下一页"
                  aria-controls="token-daily-table"
                  disabled={currentPage >= pageCount}
                  onClick={() => setPage(currentPage + 1)}
                >
                  <ChevronRight size={16} />
                </button>
              </div>
            </div>
          </section>
        </>
      ) : null}
    </>
  );
}
