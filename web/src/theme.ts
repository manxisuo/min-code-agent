import { ref } from "vue";

export type ThemeMode = "dark" | "light";

const STORAGE_KEY = "mincode-theme";

function readStored(): ThemeMode {
  try {
    const v = localStorage.getItem(STORAGE_KEY);
    if (v === "dark" || v === "light") return v;
  } catch {
    /* ignore */
  }
  if (typeof window !== "undefined" && window.matchMedia) {
    if (window.matchMedia("(prefers-color-scheme: light)").matches) return "light";
  }
  return "dark";
}

const theme = ref<ThemeMode>(readStored());

export function applyTheme(mode: ThemeMode) {
  theme.value = mode;
  document.documentElement.setAttribute("data-theme", mode);
  try {
    localStorage.setItem(STORAGE_KEY, mode);
  } catch {
    /* ignore */
  }
}

export function useTheme() {
  function toggle() {
    applyTheme(theme.value === "dark" ? "light" : "dark");
  }

  // Apply once on module load / first use.
  applyTheme(theme.value);

  return { theme, toggle };
}
