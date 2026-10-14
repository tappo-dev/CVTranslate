import { lazy, Suspense, useCallback, useEffect, useState } from "react";
import {
  ArrowUpRight,
  BarChart3,
  Clock3,
  FileText,
  Languages,
  Plug,
  Settings2,
  ShieldCheck,
} from "lucide-react";
import { errorMessage, request } from "./api";
import type { AppState, Settings } from "./types";
import { Loading, Notice, useAction } from "./ui";
import { HistoryPage } from "./HistoryPage";
import { ProvidersPage } from "./ProvidersPage";
import { PromptsPage } from "./PromptsPage";
import { SettingsPage } from "./SettingsPage";
import { TokenReportPage } from "./TokenReportPage";
import { ThemeToggle } from "./ThemeToggle";
import { repositoryUrl } from "./repository";

const TranslatePage = lazy(async () => {
  const translateModule = await import("./TranslatePage");
  return { default: translateModule.TranslatePage };
});

function GitHubMark() {
  return (
    <svg width={20} height={20} viewBox="0 0 24 24" fill="currentColor" aria-hidden="true">
      <path d="M12 .5C5.65.5.5 5.65.5 12a11.5 11.5 0 0 0 7.86 10.92c.58.1.79-.25.79-.56v-2.17c-3.2.7-3.88-1.36-3.88-1.36-.52-1.33-1.28-1.69-1.28-1.69-1.04-.71.08-.7.08-.7 1.15.08 1.76 1.19 1.76 1.19 1.03 1.76 2.7 1.25 3.36.96.1-.75.4-1.25.73-1.54-2.55-.29-5.24-1.28-5.24-5.68 0-1.26.45-2.28 1.19-3.09-.12-.29-.52-1.46.11-3.05 0 0 .97-.31 3.17 1.18a11 11 0 0 1 5.77 0c2.2-1.49 3.17-1.18 3.17-1.18.63 1.59.23 2.76.11 3.05.74.81 1.19 1.83 1.19 3.09 0 4.41-2.69 5.39-5.26 5.67.41.36.78 1.06.78 2.14v3.17c0 .31.21.67.8.56A11.5 11.5 0 0 0 23.5 12C23.5 5.65 18.35.5 12 .5Z" />
    </svg>
  );
}

const navigation = [
  {
    id: "translate",
    label: "翻译工作台",
    icon: Languages,
  },
  {
    id: "history",
    label: "翻译历史",
    icon: Clock3,
  },
  {
    id: "tokens",
    label: "Token 报表",
    icon: BarChart3,
  },
  {
    id: "providers",
    label: "服务提供商",
    icon: Plug,
  },
  {
    id: "prompts",
    label: "提示词模板",
    icon: FileText,
  },
  {
    id: "settings",
    label: "应用设置",
    icon: Settings2,
  },
] as const;
type Page = (typeof navigation)[number]["id"];

interface RouteState {
  page: Page;
  taskId: string;
  historyDateFrom: string;
  historyDateTo: string;
}

interface NavigationOptions {
  taskId?: string;
  historyDateFrom?: string;
  historyDateTo?: string;
  replace?: boolean;
}

function readHistoryDate(value: string | null) {
  if (!value || !/^\d{4}-\d{2}-\d{2}$/.test(value)) return "";
  const parsed = new Date(`${value}T00:00:00Z`);
  return Number.isNaN(parsed.getTime()) || parsed.toISOString().slice(0, 10) !== value
    ? ""
    : value;
}

function readLocation() {
  const parameters = new URLSearchParams(location.search);
  const candidate = parameters.get("page");
  return {
    page: (navigation.some((item) => item.id === candidate)
      ? candidate
      : "translate") as Page,
    taskId: parameters.get("task") || "",
    historyDateFrom: readHistoryDate(parameters.get("from")),
    historyDateTo: readHistoryDate(parameters.get("to")),
  } satisfies RouteState;
}
export type UpdateState = (state: AppState) => void;

export function App() {
  const [state, setState] = useState<AppState | null>(null);
  const [error, setError] = useState("");
  const [route, setRoute] = useState(readLocation);
  const defaultProviderAction = useAction();
  useEffect(() => {
    const controller = new AbortController();
    void request<AppState>("/state", { signal: controller.signal })
      .then((result) => {
        if (!controller.signal.aborted) setState(result);
      })
      .catch((caught) => {
        if (!controller.signal.aborted) setError(errorMessage(caught));
      });
    return () => controller.abort();
  }, []);
  useEffect(() => {
    const handlePopState = () => setRoute(readLocation());
    window.addEventListener("popstate", handlePopState);
    return () => window.removeEventListener("popstate", handlePopState);
  }, []);
  const navigate = useCallback((page: Page, options: NavigationOptions = {}) => {
    const parameters = new URLSearchParams({ page });
    if (options.taskId) parameters.set("task", options.taskId);
    if (page === "history" && options.historyDateFrom) {
      parameters.set("from", options.historyDateFrom);
    }
    if (page === "history" && options.historyDateTo) {
      parameters.set("to", options.historyDateTo);
    }
    window.history[options.replace ? "replaceState" : "pushState"](
      null,
      "",
      `?${parameters}`,
    );
    setRoute({
      page,
      taskId: options.taskId || "",
      historyDateFrom: options.historyDateFrom || "",
      historyDateTo: options.historyDateTo || "",
    });
  }, []);
  const openTask = useCallback(
    (taskId: string) => navigate("translate", { taskId }),
    [navigate],
  );
  const replaceTask = useCallback(
    (taskId: string) => navigate("translate", { taskId, replace: true }),
    [navigate],
  );
  const openHistoryDate = useCallback(
    (date: string) => navigate("history", {
      historyDateFrom: date,
      historyDateTo: date,
    }),
    [navigate],
  );
  const updateHistoryDateRange = useCallback(
    (historyDateFrom: string, historyDateTo: string) => navigate("history", {
      historyDateFrom,
      historyDateTo,
      replace: true,
    }),
    [navigate],
  );
  function saveDefaultProvider(providerId: string) {
    if (!state || defaultProviderAction.busy) return;
    defaultProviderAction.setError("");
    if (providerId === state.settings.defaultProviderId) return;
    // Own the save here so leaving the workspace does not cancel persistence.
    void defaultProviderAction.run(async (signal) => {
      const settings = await request<Settings>("/settings", {
        method: "PUT",
        body: { ...state.settings, defaultProviderId: providerId },
        signal,
      });
      if (!signal.aborted) {
        setState((current) => current ? { ...current, settings } : current);
      }
    }, "默认服务已更新");
  }
  // Configuration editors must start with the completed save, not a stale draft.
  const waitingForDefaultProvider = defaultProviderAction.busy &&
    ["settings", "providers", "prompts"].includes(route.page);
  const currentPage = navigation.find((item) => item.id === route.page)!;
  return (
    <div className="app-shell">
      <aside className="sidebar">
        <a
          className="brand"
          href="?page=translate"
          onClick={(event) => {
            event.preventDefault();
            navigate("translate");
          }}
        >
          <img className="brand-mark" src="/icon.svg" width={40} height={40} alt="" />
          <span>CVTranslate</span>
        </a>
        <div className="nav-caption">工作空间</div>
        <nav aria-label="主导航">
          {navigation.map((item) => (
            <a
              key={item.id}
              className={route.page === item.id ? "active" : ""}
              aria-current={route.page === item.id ? "page" : undefined}
              href={`?page=${item.id}`}
              onClick={(event) => {
                if (
                  event.metaKey ||
                  event.ctrlKey ||
                  event.shiftKey ||
                  event.altKey
                )
                  return;
                event.preventDefault();
                navigate(item.id);
              }}
            >
              <item.icon size={19} />
              {item.label}
            </a>
          ))}
        </nav>
        <div className="sidebar-bottom">
          <ShieldCheck size={19} />
          <strong>本地工作空间</strong>
          <p>历史仅存本机 · 翻译发送至所选服务</p>
          <span className="version">
            {state
              ? `v${state.version}${state.development ? " · 开发环境" : ""}`
              : "CVTranslate"}
          </span>
        </div>
      </aside>
      <main
        id="main-content"
        className={route.page === "translate" ? "translate-main" : undefined}
      >
        <header className="topbar">
          <span className="connection">
            <span className={`status-dot ${state ? "" : "offline"}`} />
            {state ? "本地服务已连接" : error ? "连接未完成" : "连接本地服务"}
          </span>
          <div className="topbar-actions">
            <ThemeToggle />
            <a
              className="topbar-icon-link"
              href={repositoryUrl}
              target="_blank"
              rel="noopener noreferrer"
              aria-label="GitHub 仓库"
              title="GitHub 仓库"
            >
              <GitHubMark />
            </a>
          </div>
        </header>
        <div
          className={`page-content${
            route.page === "translate" ? " translate-page-content" : ""
          }`}
        >
          <div className="page-heading">
            <div>
              <h1>{currentPage.label}</h1>
            </div>
            {route.page === "translate" && (
              <button className="subtle" onClick={() => navigate("history")}>
                查看历史 <ArrowUpRight size={16} />
              </button>
            )}
          </div>
          {defaultProviderAction.error && (
            <Notice error>
              默认服务未保存：{defaultProviderAction.error}{" "}
              {route.page !== "settings" && (
                <button onClick={() => navigate("settings")}>
                  打开应用设置
                </button>
              )}
            </Notice>
          )}
          {error ? (
            <Notice error>{error}</Notice>
          ) : !state || waitingForDefaultProvider ? (
            <Loading />
          ) : (
            <>
              {route.page === "translate" && (
                <Suspense fallback={<Loading />}>
                  <TranslatePage
                    state={state}
                    taskId={route.taskId}
                    openTask={replaceTask}
                    configure={() => navigate("providers")}
                    saveDefaultProvider={saveDefaultProvider}
                    savingDefaultProvider={defaultProviderAction.busy}
                  />
                </Suspense>
              )}
              {route.page === "history" && (
                <HistoryPage
                  openTask={openTask}
                  initialDateFrom={route.historyDateFrom}
                  initialDateTo={route.historyDateTo}
                  updateDateRange={updateHistoryDateRange}
                />
              )}
              {route.page === "tokens" && (
                <TokenReportPage openHistory={openHistoryDate} />
              )}
              {route.page === "providers" && (
                <ProvidersPage state={state} updateState={setState} />
              )}
              {route.page === "prompts" && (
                <PromptsPage state={state} updateState={setState} />
              )}
              {route.page === "settings" && (
                <SettingsPage state={state} updateState={setState} />
              )}
            </>
          )}
        </div>
      </main>
    </div>
  );
}
