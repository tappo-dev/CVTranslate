// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { ApiError, request } from "./api";
import { FeedbackProvider } from "./feedback";
import { VersionSection } from "./VersionSection";
import { createTestAppState } from "./speech-test-fixtures";

vi.mock("./api", async (importOriginal) => ({
  ...(await importOriginal<typeof import("./api")>()),
  request: vi.fn(),
}));

const repositoryUrl = "https://github.com/tappo-dev/CVTranslate";

function renderVersionSection() {
  const state = { ...createTestAppState(), version: "0.1.0", development: false };
  render(
    <FeedbackProvider>
      <VersionSection state={state} />
    </FeedbackProvider>,
  );
}

beforeEach(() => {
  vi.mocked(request).mockReset();
});
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

describe("version section", () => {
  it("shows the current version and repository link without contacting GitHub", () => {
    renderVersionSection();
    expect(screen.getByText("v0.1.0")).toBeTruthy();
    expect(screen.getByText("未检查")).toBeTruthy();
    expect(screen.getByRole("link", { name: repositoryUrl }).getAttribute("href")).toBe(repositoryUrl);
    expect(screen.queryByRole("button", { name: "更新" })).toBeNull();
    expect(request).not.toHaveBeenCalled();
  });

  it("offers an update that opens the release page after an explicit check", async () => {
    const releaseUrl = `${repositoryUrl}/releases/tag/v0.2.0`;
    const openWindow = vi.fn();
    vi.stubGlobal("open", openWindow);
    vi.mocked(request).mockResolvedValueOnce({
      currentVersion: "0.1.0",
      latestVersion: "0.2.0",
      status: "update_available",
      releaseUrl,
    });
    renderVersionSection();
    await act(async () => { fireEvent.click(screen.getByRole("button", { name: "检查更新" })); });
    expect(request).toHaveBeenCalledExactlyOnceWith("/update-check", {
      method: "POST",
      signal: expect.any(AbortSignal),
    });
    expect(screen.getByText("v0.2.0")).toBeTruthy();
    expect(screen.getByText("有可用更新")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "更新" }));
    expect(openWindow).toHaveBeenCalledExactlyOnceWith(releaseUrl, "_blank", "noopener,noreferrer");
  });

  it("reports up-to-date and no-release results without an update action", async () => {
    vi.mocked(request)
      .mockResolvedValueOnce({ currentVersion: "0.1.0", latestVersion: "0.1.0", status: "up_to_date", releaseUrl: `${repositoryUrl}/releases/tag/v0.1.0` })
      .mockResolvedValueOnce({ currentVersion: "0.1.0", latestVersion: "", status: "no_release", releaseUrl: `${repositoryUrl}/releases` });
    renderVersionSection();
    await act(async () => { fireEvent.click(screen.getByRole("button", { name: "检查更新" })); });
    expect(screen.getByText("已是最新")).toBeTruthy();
    await act(async () => { fireEvent.click(screen.getByRole("button", { name: "检查更新" })); });
    expect(screen.getByText("暂无发布版本")).toBeTruthy();
    expect(screen.queryByRole("button", { name: "更新" })).toBeNull();
  });

  it("keeps the repository link available when GitHub cannot be reached", async () => {
    vi.mocked(request).mockRejectedValueOnce(
      new ApiError("Could not connect to GitHub.", 502, "network_failed"),
    );
    renderVersionSection();
    await act(async () => { fireEvent.click(screen.getByRole("button", { name: "检查更新" })); });
    expect(screen.getByRole("alert").textContent).toBe("检查失败：无法连接 GitHub");
    expect(screen.getByText("未检查")).toBeTruthy();
    expect(screen.getByRole("link", { name: repositoryUrl })).toBeTruthy();
    expect((screen.getByRole("button", { name: "检查更新" }) as HTMLButtonElement).disabled).toBe(false);
    expect(screen.queryByRole("button", { name: "更新" })).toBeNull();
  });
});
