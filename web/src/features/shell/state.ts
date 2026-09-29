import { useCallback, useState } from "react";
import { SIDEBAR_RAIL_BELOW_PX } from "@/config";

export const SIDEBAR_KEY = "stator.sidebar";
export const GROUPS_KEY = "stator.sidebar-groups";

export type SidebarMode = "open" | "rail";

function read(key: string): string | null {
  try {
    return window.localStorage.getItem(key);
  } catch {
    return null;
  }
}

function write(key: string, value: string) {
  try {
    window.localStorage.setItem(key, value);
  } catch {
    // A browser that refuses storage still gets a sidebar; it just forgets.
  }
}

// The sidebar starts as a rail on a narrow screen and remembers what the
// reader last chose, so a laptop and a wide monitor each keep their own habit.
export function useSidebarMode(): [SidebarMode, () => void] {
  const [mode, setMode] = useState<SidebarMode>(() => {
    const stored = read(SIDEBAR_KEY);
    if (stored === "open" || stored === "rail") return stored;
    return typeof window !== "undefined" && window.innerWidth < SIDEBAR_RAIL_BELOW_PX ? "rail" : "open";
  });
  const toggle = useCallback(() => {
    setMode((current) => {
      const next = current === "open" ? "rail" : "open";
      write(SIDEBAR_KEY, next);
      return next;
    });
  }, []);
  return [mode, toggle];
}

/**
 * Which sidebar groups are folded, by id, kept per browser. Absent means the
 * group's own default, so a new group opens the way it was designed to.
 */
export function useSidebarGroups(): [Record<string, boolean>, (id: string, defaultOpen: boolean) => void] {
  const [groups, setGroups] = useState<Record<string, boolean>>(() => {
    try {
      const parsed: unknown = JSON.parse(read(GROUPS_KEY) ?? "{}");
      return parsed && typeof parsed === "object" ? (parsed as Record<string, boolean>) : {};
    } catch {
      return {};
    }
  });
  const toggle = useCallback((id: string, defaultOpen: boolean) => {
    setGroups((current) => {
      const open = current[id] ?? defaultOpen;
      const next = { ...current, [id]: !open };
      write(GROUPS_KEY, JSON.stringify(next));
      return next;
    });
  }, []);
  return [groups, toggle];
}
