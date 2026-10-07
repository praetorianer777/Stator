import { useEffect, useRef, useState, useSyncExternalStore } from "react";
import { IndexeddbPersistence } from "y-indexeddb";
import type { Awareness } from "y-protocols/awareness";
import { API_BASE, COLLAB_CONNECT_TIMEOUT_MS, COLLAB_LOCAL_PREFIX } from "@/config";
import { CollabSession, type CollabSnapshot, type CollabUser, type LocalStore, type SeedContent } from "./session";
import { collaboratorsOf, type Collaborator } from "./shared";
import { collabTransport } from "./transport";

/** Keeps each room in this browser's IndexedDB, where there is one. */
const indexedDbStore: LocalStore | null =
  typeof indexedDB === "undefined"
    ? null
    : {
        open: (room, doc) => {
          const kept = new IndexeddbPersistence(COLLAB_LOCAL_PREFIX + room, doc);
          return { destroy: () => kept.destroy(), clear: () => kept.clearData() };
        },
      };

export function collabUrl(pageId: string): string {
  const scheme = window.location.protocol === "https:" ? "wss" : "ws";
  return `${scheme}://${window.location.host}${API_BASE}/pages/${pageId}/collab`;
}

/** Editing together, editing alone because together is out of reach, or still finding out. */
export type CollabMode = "connecting" | "collab" | "solo";

const closed: CollabSnapshot = { status: "stopped", room: null, doc: null, awareness: null, base: 0, ready: false, stopped: null };
const noSession = () => () => {};

/**
 * Opens a page's shared draft, and settles on editing together once it has
 * content, or on editing alone when it does not in time. Once settled it
 * stays: the editor never changes mode under somebody typing.
 */
export function useCollab({ pageId, user, seed }: { pageId: string; user: CollabUser | null; seed: (afresh: boolean) => Promise<SeedContent> }) {
  const seedRef = useRef(seed);
  seedRef.current = seed;
  const [session, setSession] = useState<CollabSession | null>(null);
  const [mode, setMode] = useState<CollabMode>(collabTransport.enabled ? "connecting" : "solo");
  const userId = user?.id;
  const userName = user?.name;
  const userColor = user?.color;
  // Settling on solo ends the session; settling on collab keeps it.
  const solo = mode === "solo";

  useEffect(() => {
    if (!collabTransport.enabled || solo || !userId) return;
    const s = new CollabSession({
      url: collabUrl(pageId),
      user: { id: userId, name: userName ?? "", color: userColor ?? "" },
      seed: (afresh) => seedRef.current(afresh),
      socket: collabTransport.socket,
      local: indexedDbStore,
    });
    setSession(s);
    return () => {
      s.destroy();
      setSession(null);
    };
  }, [pageId, userId, userName, userColor, solo]);

  const snapshot = useSyncExternalStore(session?.subscribe ?? noSession, () => session?.state ?? closed);

  useEffect(() => {
    if (mode !== "connecting") return;
    if (snapshot.ready) setMode("collab");
    else if (snapshot.stopped) setMode("solo");
  }, [mode, snapshot.ready, snapshot.stopped]);

  useEffect(() => {
    if (mode !== "connecting") return;
    const timer = setTimeout(() => setMode((m) => (m === "connecting" ? "solo" : m)), COLLAB_CONNECT_TIMEOUT_MS);
    return () => clearTimeout(timer);
  }, [mode]);

  return { mode, snapshot, session };
}

/** Who else is in the shared draft, as their browsers say. */
export function useCollaborators(awareness: Awareness | null, selfId: string): Collaborator[] {
  const [people, setPeople] = useState<Collaborator[]>([]);
  useEffect(() => {
    if (!awareness) {
      setPeople([]);
      return;
    }
    const read = () => {
      const next = collaboratorsOf(awareness.getStates() as Map<number, Record<string, unknown>>, awareness.clientID, selfId);
      setPeople((was) => (JSON.stringify(was) === JSON.stringify(next) ? was : next));
    };
    read();
    awareness.on("change", read);
    return () => awareness.off("change", read);
  }, [awareness, selfId]);
  return people;
}
