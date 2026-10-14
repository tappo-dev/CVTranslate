import { useState } from "react";
import { ExternalLink, RefreshCw } from "lucide-react";
import { ApiError, request } from "./api";
import type { AppState, UpdateCheckResult } from "./types";
import { Notice, useAction } from "./ui";
import { repositoryUrl } from "./repository";

const updateErrorMessages: Record<string, string> = {
  network_failed: "无法连接 GitHub",
  timeout: "GitHub 响应超时",
  rate_limited: "GitHub 请求过于频繁，请稍后再试",
  invalid_response: "GitHub 返回了无法识别的版本信息",
};

function formatVersion(version: string) {
  return /^\d/.test(version) ? `v${version}` : version;
}

function describeLatestVersion(result: UpdateCheckResult | null, busy: boolean) {
  if (busy) return "正在检查…";
  if (!result) return "未检查";
  if (result.status === "no_release") return "暂无发布版本";
  return formatVersion(result.latestVersion);
}

const statusBadges: Record<UpdateCheckResult["status"], { label: string; className: string } | null> = {
  update_available: { label: "有可用更新", className: "running" },
  up_to_date: { label: "已是最新", className: "succeeded" },
  not_comparable: { label: "开发版本，无法比较", className: "unavailable" },
  no_release: null,
};

export function VersionSection({ state }: { state: AppState }) {
  const [result, setResult] = useState<UpdateCheckResult | null>(null);
  const checkAction = useAction();
  const statusBadge = result && !checkAction.busy ? statusBadges[result.status] : null;

  function checkForUpdate() {
    setResult(null);
    void checkAction.run(async (signal) => {
      try {
        const checked = await request<UpdateCheckResult>("/update-check", {
          method: "POST",
          signal,
        });
        if (!signal.aborted) setResult(checked);
      } catch (caught) {
        if (caught instanceof ApiError) {
          throw new Error(
            `检查失败：${updateErrorMessages[caught.code] ?? caught.message}`,
            { cause: caught },
          );
        }
        throw caught;
      }
    });
  }

  return (
    <section className="card settings-card">
      <div className="section-title">
        <h2>版本</h2>
      </div>
      <dl className="platform-info version-info">
        <div className="settings-row">
          <dt>当前版本</dt>
          <dd>
            {formatVersion(state.version)}
            {state.development ? " · 开发环境" : ""}
          </dd>
        </div>
        <div className="settings-row">
          <dt>最新版本</dt>
          <dd aria-live="polite">
            {describeLatestVersion(result, checkAction.busy)}
            {statusBadge && (
              <span className={`badge ${statusBadge.className}`}>{statusBadge.label}</span>
            )}
          </dd>
        </div>
        <div className="settings-row">
          <dt>GitHub</dt>
          <dd>
            <a href={repositoryUrl} target="_blank" rel="noopener noreferrer">
              {repositoryUrl}
            </a>
          </dd>
        </div>
      </dl>
      <Notice error>{checkAction.error}</Notice>
      <div className="button-row">
        <button
          type="button"
          disabled={checkAction.busy}
          aria-busy={checkAction.busy}
          onClick={checkForUpdate}
        >
          <RefreshCw size={16} aria-hidden="true" />
          {checkAction.busy ? "正在检查…" : "检查更新"}
        </button>
        {result?.status === "update_available" && !checkAction.busy && (
          <button
            type="button"
            className="primary"
            onClick={() => window.open(result.releaseUrl, "_blank", "noopener,noreferrer")}
          >
            <ExternalLink size={16} aria-hidden="true" />
            更新
          </button>
        )}
      </div>
    </section>
  );
}
