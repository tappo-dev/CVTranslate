// @vitest-environment jsdom
import { readFileSync } from "node:fs";
import { join } from "node:path";
import { act, cleanup, fireEvent, render, screen, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { request } from "./api";
import { App } from "./App";
import { FeedbackProvider } from "./feedback";
import { HistoryPage } from "./HistoryPage";
import { createTestAppState } from "./speech-test-fixtures";
import { TokenReportPage } from "./TokenReportPage";
import type { DailyTokenUsage, Task, TokenReport, TokenUsageSummary } from "./types";

vi.mock("./api", async (importOriginal) => ({
  ...(await importOriginal<typeof import("./api")>()),
  request: vi.fn(),
}));

beforeEach(() => {
  vi.mocked(request).mockReset();
  window.history.replaceState(null, "", "/");
});

afterEach(() => {
  cleanup();
  window.history.replaceState(null, "", "/");
});

function createHistoryTask(): Task {
  return {
    id: "history-task",
    text: "Original text",
    translation: "Translated text",
    sourceLanguage: "en",
    targetLanguage: "zh-CN",
    entry: "web",
    status: "succeeded",
    providerSnapshot: {
      id: "provider",
      name: "AI provider",
      type: "openai-compatible",
      baseUrl: "https://example.test/v1",
      model: "gpt-6-luna",
      timeoutSeconds: 30,
      enabled: true,
      hasApiKey: true,
    },
    promptSnapshot: {
      id: "builtin-general",
      name: "General translation",
      systemTemplate: "Translate.",
      userTemplate: "{{text}}",
      persona: "Serious developer",
      revision: 2,
      builtIn: true,
    },
    errorCode: "",
    errorMessage: "",
    pasteStatus: "",
    createdAt: "2026-10-14T08:00:00Z",
    updatedAt: "2026-10-14T08:00:01Z",
    durationMs: 1000,
    tokenUsage: {
      promptTokens: 80,
      completionTokens: 20,
      totalTokens: 100,
    },
  };
}

describe("translation history actions", () => {
  it("keeps confirmed single-record deletion without a bulk-clear action", async () => {
    vi.mocked(request)
      .mockResolvedValueOnce({
        items: [createHistoryTask()], page: 1, pageSize: 20, total: 1,
      })
      .mockResolvedValueOnce(undefined)
      .mockResolvedValueOnce({ items: [], page: 1, pageSize: 20, total: 0 });
    render(
      <FeedbackProvider>
        <HistoryPage openTask={vi.fn()} />
      </FeedbackProvider>,
    );
    await act(async () => Promise.resolve());
    expect(screen.queryByRole("button", { name: "清空历史" })).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: /Original text/ }));
    fireEvent.click(screen.getByRole("button", { name: "删除" }));
    const confirmation = screen.getByRole("alertdialog", { name: "删除这条翻译记录？" });
    expect(request).toHaveBeenCalledOnce();
    await act(async () => {
      fireEvent.click(within(confirmation).getByRole("button", { name: "删除" }));
    });
    expect(request).toHaveBeenNthCalledWith(2, "/history/history-task", {
      method: "DELETE",
      signal: expect.any(AbortSignal),
    });
    expect(screen.getByText("暂无翻译记录")).toBeTruthy();
    expect(screen.queryByRole("heading", { name: "任务详情" })).toBeNull();
    expect(request).not.toHaveBeenCalledWith("/history", expect.anything());
  });

  it("copies original and translated text from their leading icons", async () => {
    const writeText = vi.fn().mockResolvedValue(undefined);
    Object.defineProperty(navigator, "clipboard", {
      configurable: true,
      value: { writeText },
    });
    vi.mocked(request).mockResolvedValueOnce({
      items: [createHistoryTask()],
      page: 1,
      pageSize: 20,
      total: 1,
    });
    render(
      <FeedbackProvider>
        <HistoryPage openTask={vi.fn()} />
      </FeedbackProvider>,
    );
    await act(async () => Promise.resolve());
    fireEvent.click(screen.getByRole("button", { name: "复制原文" }));
    fireEvent.click(screen.getByRole("button", { name: "复制译文" }));
    await act(async () => Promise.resolve());
    expect(writeText).toHaveBeenNthCalledWith(1, "Original text");
    expect(writeText).toHaveBeenNthCalledWith(2, "Translated text");
  });

  it("shows token usage in the selected history detail", async () => {
    vi.mocked(request).mockResolvedValueOnce({
      items: [createHistoryTask()],
      page: 1,
      pageSize: 20,
      total: 1,
    });
    render(
      <FeedbackProvider>
        <HistoryPage openTask={vi.fn()} />
      </FeedbackProvider>,
    );
    await act(async () => Promise.resolve());
    fireEvent.click(screen.getByRole("button", { name: /Original text/ }));
    expect(screen.getByText(/100（输入80 \/ 输出20）/)).toBeTruthy();
  });

  it("filters history by a local date range and can clear it", async () => {
    const updateDateRange = vi.fn();
    vi.mocked(request)
      .mockResolvedValueOnce({ items: [], page: 1, pageSize: 20, total: 0 })
      .mockResolvedValueOnce({ items: [], page: 1, pageSize: 20, total: 0 })
      .mockResolvedValueOnce({ items: [], page: 1, pageSize: 20, total: 0 });
    render(
      <FeedbackProvider>
        <HistoryPage openTask={vi.fn()} updateDateRange={updateDateRange} />
      </FeedbackProvider>,
    );
    await act(async () => Promise.resolve());

    fireEvent.change(screen.getByLabelText("开始日期"), {
      target: { value: "2026-10-13" },
    });
    fireEvent.change(screen.getByLabelText("结束日期"), {
      target: { value: "2026-10-14" },
    });
    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: "筛选" }));
    });

    const expectedStart = new Date(2026, 9, 13).toISOString();
    const expectedEnd = new Date(2026, 9, 15).toISOString();
    expect(request).toHaveBeenLastCalledWith(
      `/history?page=1&pageSize=20&q=&status=&createdAfter=${encodeURIComponent(expectedStart)}&createdBefore=${encodeURIComponent(expectedEnd)}`,
      expect.objectContaining({ signal: expect.any(AbortSignal) }),
    );
    expect(updateDateRange).toHaveBeenLastCalledWith("2026-10-13", "2026-10-14");

    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: "清除日期" }));
    });
    expect(request).toHaveBeenLastCalledWith(
      "/history?page=1&pageSize=20&q=&status=",
      expect.objectContaining({ signal: expect.any(AbortSignal) }),
    );
    expect(updateDateRange).toHaveBeenLastCalledWith("", "");
  });
});

describe("token report", () => {
  it("navigates to the selected history date and restores it from the URL", async () => {
    const report = createTokenReport([{
      date: "2026-10-14",
      requestCount: 1,
      promptTokens: 80,
      completionTokens: 20,
      totalTokens: 100,
    }]);
    vi.mocked(request).mockImplementation(async (path) => {
      if (path === "/state") return createTestAppState();
      if (path.startsWith("/token-report?")) return report;
      if (path.startsWith("/history?")) {
        return { items: [createHistoryTask()], page: 1, pageSize: 20, total: 1 };
      }
      throw new Error(`Unexpected request: ${path}`);
    });
    window.history.replaceState(null, "", "?page=tokens");
    const application = render(
      <FeedbackProvider><App /></FeedbackProvider>,
    );
    const dayCell = await screen.findByRole("button", {
      name: "2026/10/14：100 Token · 1 次",
    });
    fireEvent.click(dayCell, { detail: 1 });
    expect(screen.getByRole("heading", { level: 1, name: "Token 报表" })).toBeTruthy();
    await act(async () => { fireEvent.doubleClick(dayCell); });
    expect(screen.getByRole("heading", { level: 1, name: "翻译历史" })).toBeTruthy();
    expect(window.location.search).toBe("?page=history&from=2026-10-14&to=2026-10-14");
    const historyRequests = vi.mocked(request).mock.calls.filter(([path]) =>
      path.startsWith("/history?"),
    );
    expect(historyRequests).toHaveLength(1);
    const parameters = new URLSearchParams(historyRequests[0][0].split("?")[1]);
    expect(parameters.get("createdAfter")).toBe(new Date(2026, 9, 14).toISOString());
    expect(parameters.get("createdBefore")).toBe(new Date(2026, 9, 15).toISOString());

    application.unmount();
    render(<FeedbackProvider><App /></FeedbackProvider>);
    const dateFrom = await screen.findByLabelText<HTMLInputElement>("开始日期");
    expect(dateFrom.value).toBe("2026-10-14");
    expect(screen.getByLabelText<HTMLInputElement>("结束日期").value).toBe("2026-10-14");
    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: "清除日期" }));
    });
    expect(window.location.search).toBe("?page=history");
    expect(request).toHaveBeenLastCalledWith(
      "/history?page=1&pageSize=20&q=&status=",
      expect.objectContaining({ signal: expect.any(AbortSignal) }),
    );
  });

  it("renders the heatmap, summaries, and daily table", async () => {
    const report: TokenReport = {
      today: { requestCount: 1, promptTokens: 80, completionTokens: 20, totalTokens: 100 },
      last7Days: { requestCount: 2, promptTokens: 200, completionTokens: 50, totalTokens: 250 },
      last30Days: { requestCount: 3, promptTokens: 400, completionTokens: 100, totalTokens: 500 },
      daily: [
        { date: "2026-10-13", requestCount: 2, promptTokens: 120, completionTokens: 30, totalTokens: 150 },
        { date: "2026-10-14", requestCount: 1, promptTokens: 80, completionTokens: 20, totalTokens: 100 },
      ],
    };
    vi.mocked(request).mockResolvedValueOnce(report);
    render(<TokenReportPage openHistory={vi.fn()} />);
    await act(async () => Promise.resolve());
    expect(screen.getByLabelText("每日 Token 热力图")).toBeTruthy();
    expect(screen.getByLabelText("Token 汇总").textContent).toContain("近一月");
    expect(screen.getByRole("table", { name: "每日 Token 报表" })).toBeTruthy();
    expect(request).toHaveBeenCalledWith(
      expect.stringMatching(/^\/token-report\?days=365&timezoneOffsetMinutes=-?\d+$/),
      expect.objectContaining({ signal: expect.any(AbortSignal) }),
    );
  });

  it("paginates only recorded days, keeps the full heatmap, and resets after refresh", async () => {
    const daily = Array.from({ length: 32 }, (_, dayIndex): DailyTokenUsage => ({
      date: new Date(Date.UTC(2026, 0, 20 + dayIndex)).toISOString().slice(0, 10),
      requestCount: dayIndex % 3 === 0 ? 0 : 1,
      promptTokens: dayIndex % 3 === 0 || dayIndex === 1 ? 0 : 80,
      completionTokens: dayIndex % 3 === 0 || dayIndex === 1 ? 0 : 20,
      totalTokens: dayIndex % 3 === 0 || dayIndex === 1 ? 0 : 100,
    }));
    const report = createTokenReport(daily);
    vi.mocked(request).mockResolvedValueOnce(report);
    render(<TokenReportPage openHistory={vi.fn()} />);
    const table = await screen.findByRole("table", { name: "每日 Token 报表" });
    const previous = screen.getByRole<HTMLButtonElement>("button", { name: "上一页" });
    const next = screen.getByRole<HTMLButtonElement>("button", { name: "下一页" });
    const summaryText = screen.getByLabelText("Token 汇总").textContent;

    expect(table.querySelectorAll("time")).toHaveLength(10);
    expect(table.querySelector("time")?.dateTime).toBe("2026-02-20");
    expect(table.querySelector('time[datetime="2026-02-19"]')).toBeNull();
    expect(screen.getByText("共 21 天 · 每页 10 条")).toBeTruthy();
    expect(previous.disabled).toBe(true);
    expect(next.disabled).toBe(false);

    fireEvent.click(next);
    expect(table.querySelectorAll("time")).toHaveLength(10);
    expect(screen.getByText("第 2 / 3 页")).toBeTruthy();
    fireEvent.click(next);
    expect(table.querySelectorAll("time")).toHaveLength(1);
    // A real response reporting zero tokens is still a recorded day.
    expect(table.querySelector("time")?.dateTime).toBe("2026-01-21");
    expect(next.disabled).toBe(true);
    expect(previous.disabled).toBe(false);
    expect(screen.getByLabelText("Token 汇总").textContent).toBe(summaryText);
    expect(
      screen
        .getByLabelText("每日 Token 热力图")
        .querySelectorAll(".token-heatmap .heatmap-cell"),
    ).toHaveLength(32);
    expect(request).toHaveBeenCalledTimes(1);
    fireEvent.click(previous);
    expect(screen.getByText("第 2 / 3 页")).toBeTruthy();

    vi.mocked(request).mockResolvedValueOnce(createTokenReport(daily.slice(-2)));
    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: "刷新 Token 报表" }));
    });
    expect(screen.getByText("第 1 / 1 页")).toBeTruthy();
    expect(screen.getByText("共 1 天 · 每页 10 条")).toBeTruthy();
    expect(screen.getByRole<HTMLButtonElement>("button", { name: "下一页" }).disabled).toBe(true);
  });

  it("shows an empty detail table instead of a year of zero rows", async () => {
    const daily = Array.from({ length: 365 }, (_, dayIndex): DailyTokenUsage => ({
      date: new Date(Date.UTC(2025, 9, 15 + dayIndex)).toISOString().slice(0, 10),
      requestCount: 0,
      promptTokens: 0,
      completionTokens: 0,
      totalTokens: 0,
    }));
    vi.mocked(request).mockResolvedValueOnce(createTokenReport(daily));
    render(<TokenReportPage openHistory={vi.fn()} />);
    const table = await screen.findByRole("table", { name: "每日 Token 报表" });
    expect(within(table).getByText("暂无 Token 记录")).toBeTruthy();
    expect(table.querySelectorAll("time")).toHaveLength(0);
    expect(screen.getByText("共 0 天 · 每页 10 条")).toBeTruthy();
    expect(screen.getByRole<HTMLButtonElement>("button", { name: "上一页" }).disabled).toBe(true);
    expect(screen.getByRole<HTMLButtonElement>("button", { name: "下一页" }).disabled).toBe(true);

    const heatmap = within(screen.getByLabelText("每日 Token 热力图"));
    expect(heatmap.getAllByRole("img")).toHaveLength(365);
    expect(heatmap.getByText("1月")).toBeTruthy();
    expect(heatmap.getByText("周一")).toBeTruthy();
    expect(heatmap.getByText("周三")).toBeTruthy();
    expect(heatmap.getByText("周五")).toBeTruthy();
    const firstDay = heatmap.getByRole("img", { name: "2025/10/15：0 Token · 0 次" });
    expect(firstDay.style.gridColumn).toBe("2");
    expect(firstDay.style.gridRow).toBe("5");
    const nextSunday = heatmap.getByRole("img", { name: "2025/10/19：0 Token · 0 次" });
    expect(nextSunday.style.gridColumn).toBe("3");
    expect(nextSunday.style.gridRow).toBe("2");
    expect(nextSunday.classList.contains("level-0")).toBe(true);

    // Check the actual cascade; jsdom does not measure grid layout dimensions.
    const stylesheet = document.createElement("style");
    stylesheet.textContent = readFileSync(join(process.cwd(), "src", "styles.css"), "utf8");
    document.head.append(stylesheet);
    try {
      const calendarStyles = getComputedStyle(firstDay.parentElement!);
      expect(calendarStyles.width).toBe("100%");
      expect(calendarStyles.gridTemplateColumns).toBe(
        "36px repeat(53, minmax(var(--heatmap-cell-size), 1fr))",
      );
      expect(calendarStyles.gridTemplateRows).toBe("24px repeat(7, auto)");
      const cellStyles = getComputedStyle(firstDay);
      expect(cellStyles.width).toBe("100%");
      expect(cellStyles.height).toBe("auto");
      expect(cellStyles.aspectRatio).toBe("1");
      const legendCell = heatmap.getByLabelText("热力图图例").querySelector("i")!;
      expect(getComputedStyle(legendCell).width).toBe("var(--heatmap-cell-size)");
      expect(getComputedStyle(legendCell).height).toBe("var(--heatmap-cell-size)");
      expect(getComputedStyle(screen.getByLabelText("每日 Token 热力图"))
        .getPropertyValue("--heatmap-cell-size")).toBe("12px");
    } finally {
      stylesheet.remove();
    }
  });

  it("shows the day's usage on keyboard focus and mouse hover", async () => {
    vi.mocked(request).mockResolvedValueOnce(createTokenReport([{
      date: "2026-10-14",
      requestCount: 1,
      promptTokens: 80,
      completionTokens: 20,
      totalTokens: 100,
    }]));
    render(<TokenReportPage openHistory={vi.fn()} />);
    const heatmap = within(await screen.findByLabelText("每日 Token 热力图"));
    const dayCell = heatmap.getByRole("button", { name: "2026/10/14：100 Token · 1 次" });
    expect(dayCell.tabIndex).toBe(0);
    fireEvent.focus(dayCell);
    expect(heatmap.getByText("2026/10/14：100 Token · 1 次")).toBeTruthy();
    fireEvent.blur(dayCell);
    expect(heatmap.getByText("1 天有记录")).toBeTruthy();
    fireEvent.mouseEnter(dayCell);
    expect(heatmap.getByText("2026/10/14：100 Token · 1 次")).toBeTruthy();
    fireEvent.mouseLeave(dayCell);
    expect(heatmap.getByText("1 天有记录")).toBeTruthy();
  });

  it("opens the selected day's history on heatmap double-click", async () => {
    const openHistory = vi.fn();
    vi.mocked(request).mockResolvedValueOnce(createTokenReport([{
      date: "2026-10-14",
      requestCount: 1,
      promptTokens: 80,
      completionTokens: 20,
      totalTokens: 100,
    }]));
    render(<TokenReportPage openHistory={openHistory} />);
    const dayCell = await screen.findByRole("button", {
      name: "2026/10/14：100 Token · 1 次",
    });
    fireEvent.doubleClick(dayCell);
    expect(openHistory).toHaveBeenCalledExactlyOnceWith("2026-10-14");
  });
});

function createTokenReport(daily: DailyTokenUsage[]): TokenReport {
  function summarize(days: DailyTokenUsage[]): TokenUsageSummary {
    return days.reduce((summary, day) => ({
      requestCount: summary.requestCount + day.requestCount,
      promptTokens: summary.promptTokens + day.promptTokens,
      completionTokens: summary.completionTokens + day.completionTokens,
      totalTokens: summary.totalTokens + day.totalTokens,
    }), { requestCount: 0, promptTokens: 0, completionTokens: 0, totalTokens: 0 });
  }
  return {
    today: summarize(daily.slice(-1)),
    last7Days: summarize(daily.slice(-7)),
    last30Days: summarize(daily.slice(-30)),
    daily,
  };
}
