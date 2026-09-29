import { useLayoutEffect, useRef, type KeyboardEvent, type ReactNode } from "react";
import { cx } from "@/components/ui";

const ITEMS = "button:not([disabled]), select:not([disabled]), input:not([disabled])";

function setTabStop(items: HTMLElement[], stop: number) {
  for (const [i, el] of items.entries()) el.setAttribute("tabindex", i === stop ? "0" : "-1");
}

/**
 * A toolbar that is one stop in the Tab order: arrow keys, Home and End move
 * between its controls, and Tab leaves it from the control last used.
 */
export function RovingToolbar({
  label,
  children,
  className,
  ...rest
}: {
  label: string;
  children: ReactNode;
  className?: string;
  [data: `data-${string}`]: string | boolean;
}) {
  const ref = useRef<HTMLDivElement>(null);
  const current = useRef(0);

  // Runs after every render because the controls come and go with the selection.
  useLayoutEffect(() => {
    const items = Array.from(ref.current?.querySelectorAll<HTMLElement>(ITEMS) ?? []);
    if (current.current >= items.length) current.current = Math.max(0, items.length - 1);
    setTabStop(items, current.current);
  });

  function onKeyDown(event: KeyboardEvent<HTMLDivElement>) {
    const items = Array.from(ref.current?.querySelectorAll<HTMLElement>(ITEMS) ?? []);
    const index = items.indexOf(document.activeElement as HTMLElement);
    if (index < 0) return;
    let next: number;
    switch (event.key) {
      case "ArrowRight":
        next = (index + 1) % items.length;
        break;
      case "ArrowLeft":
        next = (index - 1 + items.length) % items.length;
        break;
      case "Home":
        next = 0;
        break;
      case "End":
        next = items.length - 1;
        break;
      default:
        return;
    }
    // A text box keeps its own arrow keys.
    if (items[index]?.tagName === "INPUT") return;
    event.preventDefault();
    focusItem(items, next);
  }

  function focusItem(items: HTMLElement[], i: number) {
    setTabStop(items, i);
    current.current = i;
    items[i]?.focus();
  }

  return (
    <div
      {...rest}
      ref={ref}
      role="toolbar"
      aria-label={label}
      aria-orientation="horizontal"
      onKeyDown={onKeyDown}
      onFocus={(event) => {
        const items = Array.from(ref.current?.querySelectorAll<HTMLElement>(ITEMS) ?? []);
        const i = items.indexOf(event.target as HTMLElement);
        if (i >= 0 && i !== current.current) focusItem(items, i);
      }}
      className={cx("flex flex-wrap items-center gap-0.5", className)}
    >
      {children}
    </div>
  );
}
