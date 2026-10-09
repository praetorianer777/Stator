import { useEffect, useState } from "react";
import type { Awareness } from "y-protocols/awareness";
import { Avatar } from "@/components/ui";
import { t } from "@/i18n";
import { sketchDrawers } from "./live";

type Drawer = { id: string; name: string; color: string };

/** Who else has a sketch open right now. */
export function useSketchDrawers(awareness: Awareness | null, sketchId: string | null): Drawer[] {
  const [drawers, setDrawers] = useState<Drawer[]>([]);
  useEffect(() => {
    if (!awareness || !sketchId) {
      setDrawers([]);
      return;
    }
    const read = () => {
      const next = sketchDrawers(awareness.getStates() as Map<number, Record<string, unknown>>, awareness.clientID, sketchId);
      setDrawers((was) => (JSON.stringify(was) === JSON.stringify(next) ? was : next));
    };
    read();
    awareness.on("change", read);
    return () => awareness.off("change", read);
  }, [awareness, sketchId]);
  return drawers;
}

/** The people drawing on a sketch with this person, as coloured avatars named for screen readers. */
export function SketchDrawers({ drawers }: { drawers: Drawer[] }) {
  if (drawers.length === 0) return null;
  const names = drawers.map((d) => d.name).join(", ");
  return (
    <ul className="inline-flex items-center -space-x-1" aria-label={t.editor.sketch.drawingNow(names)} data-sketch-drawers={names}>
      {drawers.map((d) => (
        <li key={d.id} data-sketch-drawer={d.id}>
          <Avatar name={d.name} color={d.color} size="md" className="ring-2 ring-surface" />
        </li>
      ))}
    </ul>
  );
}
