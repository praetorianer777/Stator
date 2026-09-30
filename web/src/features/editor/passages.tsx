import { createContext, useContext, type ReactNode } from "react";

/** What the reader needs to draw a page's passages and open their threads. */
export interface Passages {
  /** Threads whose passage is highlighted; a mark naming any other draws nothing. */
  shown: ReadonlySet<string>;
  active?: string;
  open: (threadId: string) => void;
}

export const PassagesContext = createContext<Passages | null>(null);

/** Whether the document being drawn is a page whose passages take comments. */
export function usePassages(): Passages | null {
  return useContext(PassagesContext);
}

/** A block's place in its document: the index at each level, as the data-block attribute holds it. */
export type BlockPath = readonly number[];

/** Text marked for a thread, drawn only while that thread is shown. */
export function Passage({ threadId, children }: { threadId: string; children: ReactNode }) {
  const passages = usePassages();
  if (!passages?.shown.has(threadId)) return children;
  const active = passages.active === threadId;
  return (
    // biome-ignore lint/a11y/useKeyWithClickEvents: the list of passages under the page opens every thread from the keyboard
    <mark
      className="doc-passage"
      data-passage={threadId}
      data-active={active || undefined}
      onClick={(event) => {
        // The innermost passage is the one clicked; those around it stay closed.
        event.stopPropagation();
        passages.open(threadId);
      }}
    >
      {children}
    </mark>
  );
}
