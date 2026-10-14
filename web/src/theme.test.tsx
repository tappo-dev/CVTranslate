// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { FeedbackProvider } from "./feedback";
import { ThemeToggle } from "./ThemeToggle";
import { applyTheme, readTheme } from "./theme";

beforeEach(() => {
  window.localStorage.clear();
  applyTheme("light");
});

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
  window.localStorage.clear();
  document.documentElement.removeAttribute("data-theme");
  document.documentElement.classList.remove("dark-theme", "unrelated-class");
});

describe("theme preference", () => {
  it("switches both ways and restores the saved choice on a new mount", () => {
    document.documentElement.classList.add("unrelated-class");
    const firstView = render(<FeedbackProvider><ThemeToggle /></FeedbackProvider>);
    const toggle = screen.getByRole("button", { name: "切换到暗色模式" });
    toggle.focus();
    fireEvent.click(toggle);
    expect(document.documentElement.dataset.theme).toBe("dark");
    expect(document.documentElement.classList.contains("dark-theme")).toBe(true);
    expect(document.activeElement).toBe(toggle);
    expect(readTheme()).toBe("dark");
    firstView.unmount();

    applyTheme("light");
    render(<FeedbackProvider><ThemeToggle /></FeedbackProvider>);
    expect(document.documentElement.dataset.theme).toBe("dark");
    fireEvent.click(screen.getByRole("button", { name: "切换到亮色模式" }));
    expect(document.documentElement.dataset.theme).toBe("light");
    expect(document.documentElement.classList.contains("dark-theme")).toBe(false);
    expect(document.documentElement.classList.contains("unrelated-class")).toBe(true);
    expect(readTheme()).toBe("light");
  });

  it.each([null, "unknown", "system"])("defaults to light for stored value %s", (storedTheme) => {
    if (storedTheme !== null) window.localStorage.setItem("cvtranslate.theme", storedTheme);
    expect(readTheme()).toBe("light");
  });

  it("still switches when storage is blocked and reports the persistence failure", () => {
    vi.spyOn(Storage.prototype, "getItem").mockImplementation(() => {
      throw new DOMException("Storage blocked", "SecurityError");
    });
    vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => {
      throw new DOMException("Storage blocked", "SecurityError");
    });
    render(<FeedbackProvider><ThemeToggle /></FeedbackProvider>);
    fireEvent.click(screen.getByRole("button", { name: "切换到暗色模式" }));
    expect(document.documentElement.dataset.theme).toBe("dark");
    expect(screen.getByText("主题已切换，无法记住选择")).toBeTruthy();
    expect(screen.getByRole("button", { name: "切换到亮色模式" })).toBeTruthy();
  });
});
