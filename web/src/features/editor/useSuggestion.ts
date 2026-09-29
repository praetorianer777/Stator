import { useCallback, useMemo, useRef, useState } from "react";
import type { SuggestionKeyDownProps, SuggestionProps } from "@tiptap/suggestion";

export interface Suggesting<T> {
  items: T[];
  rect: DOMRect | null;
  pick: (item: T) => void;
}

/**
 * The state behind a list drawn under the caret: which items, which one is
 * active, and the keys that move and pick. The editor keeps focus throughout,
 * so the list is driven from the editor's own key handling.
 */
export function useSuggestion<T, P = T>(toCommand: (item: T) => P) {
  const [open, setOpen] = useState<Suggesting<T> | null>(null);
  const [active, setActiveState] = useState(0);
  const current = useRef<Suggesting<T> | null>(null);
  const activeRef = useRef(0);
  const convert = useRef(toCommand);
  convert.current = toCommand;

  const setActive = useCallback((i: number) => {
    activeRef.current = i;
    setActiveState(i);
  }, []);

  const renderer = useMemo(() => {
    const show = (props: SuggestionProps<T, P>) => {
      const next: Suggesting<T> = { items: props.items, rect: props.clientRect?.() ?? null, pick: (item) => props.command(convert.current(item)) };
      current.current = next;
      activeRef.current = 0;
      setActiveState(0);
      setOpen(next);
    };
    return () => ({
      onStart: show,
      onUpdate: show,
      onKeyDown: ({ event }: SuggestionKeyDownProps) => {
        const list = current.current;
        if (!list || list.items.length === 0) return false;
        const move = (delta: number) => setActive((activeRef.current + delta + list.items.length) % list.items.length);
        switch (event.key) {
          case "ArrowDown":
            move(1);
            return true;
          case "ArrowUp":
            move(-1);
            return true;
          case "Enter":
          case "Tab": {
            const item = list.items[activeRef.current];
            if (item !== undefined) list.pick(item);
            return true;
          }
        }
        return false;
      },
      onExit: () => {
        current.current = null;
        setOpen(null);
      },
    });
  }, [setActive]);

  return { open, active, setActive, renderer };
}
