import { SETTINGS_LEAVE_FALLBACK, SETTINGS_RETURN_KEY } from "@/config";

/** Notes a page outside the settings area, which "Back to Stator" returns to. */
export function rememberOutsideSettings(href: string) {
  try {
    window.sessionStorage.setItem(SETTINGS_RETURN_KEY, href);
  } catch {
    // A browser that refuses storage goes back to the start instead.
  }
}

/** The last page outside the settings area in this tab, or the start. */
export function settingsReturn(): string {
  try {
    return window.sessionStorage.getItem(SETTINGS_RETURN_KEY) ?? SETTINGS_LEAVE_FALLBACK;
  } catch {
    return SETTINGS_LEAVE_FALLBACK;
  }
}
