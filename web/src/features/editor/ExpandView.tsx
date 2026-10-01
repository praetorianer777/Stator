import { useEffect, useId, useRef, useState, type ReactNode } from "react";
import { flushSync } from "react-dom";
import { Icon } from "@/components/icons";
import { t } from "@/i18n";

/** Asks a closed expand block to open, so something inside it can be shown. */
const REVEAL_EVENT = "doc-expand-reveal";

/** The words on an expand block's toggle: its title, or a stock one. */
export function expandTitle(title: unknown): string {
  return typeof title === "string" && title.trim() ? title : t.editor.expand.untitled;
}

/** Opens every closed expand block around an element; false when there was none. */
export function revealInExpands(target: Element): boolean {
  let opened = false;
  for (
    let el = target.parentElement?.closest('[data-expand][data-expanded="false"]');
    el;
    el = el.parentElement?.closest('[data-expand][data-expanded="false"]')
  ) {
    el.dispatchEvent(new Event(REVEAL_EVENT));
    opened = true;
  }
  return opened;
}

/**
 * A titled section the reader opens and closes; closed until they do, unless
 * the view shows everything. Printing shows it open whatever its state.
 */
export function ExpandView({ title, initiallyOpen = false, children }: { title: unknown; initiallyOpen?: boolean; children: ReactNode }) {
  const [open, setOpen] = useState(initiallyOpen);
  const id = useId();
  const root = useRef<HTMLDivElement>(null);
  useEffect(() => {
    const el = root.current;
    // Synchronous, so whoever asked can scroll to what is now laid out.
    const reveal = () => flushSync(() => setOpen(true));
    el?.addEventListener(REVEAL_EVENT, reveal);
    return () => el?.removeEventListener(REVEAL_EVENT, reveal);
  }, []);
  const text = expandTitle(title);
  return (
    <div ref={root} className="doc-expand" data-expand="" data-title={typeof title === "string" ? title : ""} data-expanded={open}>
      <button type="button" className="doc-expand-toggle" aria-expanded={open} aria-controls={id} onClick={() => setOpen((was) => !was)} data-expand-toggle="">
        <Icon.ChevronDown className="doc-expand-chevron" />
        <span>{text}</span>
      </button>
      <div id={id} className="doc-expand-body" data-expand-body="">
        {children}
      </div>
    </div>
  );
}
