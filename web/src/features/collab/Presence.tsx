import type { Awareness } from "y-protocols/awareness";
import { Avatar } from "@/components/ui";
import { COLLAB_AVATARS_SHOWN } from "@/config";
import { t } from "@/i18n";
import type { CollabStatus } from "./session";
import { useCollaborators } from "./useCollab";

/** Who else is editing, as coloured avatars, and whether the shared draft is reached. */
export function Presence({ awareness, selfId, status }: { awareness: Awareness | null; selfId: string; status: CollabStatus }) {
  const people = useCollaborators(awareness, selfId);
  const shown = people.slice(0, COLLAB_AVATARS_SHOWN);
  const more = people.length - shown.length;
  return (
    <span className="inline-flex items-center gap-2" data-presence={status}>
      {people.length > 0 && (
        <ul className="inline-flex items-center -space-x-1" aria-label={t.collab.editingNow(people.map((p) => p.name).join(", "))}>
          {shown.map((p) => (
            <li key={p.id} data-collaborator={p.id}>
              <Avatar name={p.name} color={p.color} size="md" className="ring-2 ring-surface" />
            </li>
          ))}
          {more > 0 && <li className="pl-2 text-xs text-ink-subtle">{t.collab.more(more)}</li>}
        </ul>
      )}
      {status !== "live" && <span className="text-xs text-ink-subtle">{status === "offline" ? t.collab.offline : t.collab.connecting}</span>}
    </span>
  );
}
