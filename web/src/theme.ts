export type Theme = "light" | "dark";

const themeStorageKey = "cvtranslate.theme";

export function readTheme(): Theme {
  try {
    return window.localStorage.getItem(themeStorageKey) === "dark" ? "dark" : "light";
  } catch {
    return "light";
  }
}

export function applyTheme(theme: Theme) {
  document.documentElement.dataset.theme = theme;
  // MDXEditor's palette, including its portals, inherits this root class.
  document.documentElement.classList.toggle("dark-theme", theme === "dark");
}

export function saveTheme(theme: Theme) {
  window.localStorage.setItem(themeStorageKey, theme);
}
