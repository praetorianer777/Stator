import { useCallback, useState, type ReactNode } from "react";
import { useNavigate } from "@tanstack/react-router";
import { ShellHeaderContext } from "@/components/ui";
import { Sidebar } from "./Sidebar";
import { TopBar, useSearchShortcut } from "./TopBar";

/** The chrome every page sits in: rail, sidebar, top bar, and the content with its floating head. */
export function AppShell({ children }: { children: ReactNode }) {
  const navigate = useNavigate();
  const openSearch = useCallback(() => navigate({ to: "/search" }), [navigate]);
  useSearchShortcut(openSearch);
  // The page's head is drawn into the strip over the content, which floats
  // as the content scrolls under it; the ref lands before first paint.
  const [strip, setStrip] = useState<HTMLElement | null>(null);
  return (
    <div className="flex h-full">
      <Sidebar />
      <div className="flex min-w-0 flex-1 flex-col">
        <TopBar />
        <main className="relative isolate min-h-0 flex-1 overflow-auto bg-backdrop">
          <div
            ref={setStrip}
            className="sticky top-0 z-20 border-b border-border/60 bg-surface-glass px-8 pt-5 backdrop-blur-md empty:hidden"
            data-shell-header
          />
          <ShellHeaderContext.Provider value={strip}>
            <div className="px-8 py-6">{children}</div>
          </ShellHeaderContext.Provider>
        </main>
      </div>
    </div>
  );
}
