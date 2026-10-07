import * as Y from "yjs";
import { Awareness, applyAwarenessUpdate, encodeAwarenessUpdate, removeAwarenessStates } from "y-protocols/awareness";
import { COLLAB_RECONNECT_MAX_MS, COLLAB_RECONNECT_MIN_MS, COLLAB_SEED_WAIT_MS } from "@/config";
import {
  CLOSE,
  decodeServer,
  encodeAwareness,
  encodeCompacted,
  encodeDiscard,
  encodePublished,
  encodeQueryAwareness,
  encodeSeed,
  encodeUpdate,
  isEmptyUpdate,
} from "./protocol";
import { isSeeded, markSeeded } from "./shared";

/** Where a session stands: reaching the server, in the room, cut off, or stopped for good. */
export type CollabStatus = "connecting" | "live" | "offline" | "stopped";

/** Why a session stopped for good. */
export type StopReason = "signedOut" | "refused";

export interface CollabUser {
  id: string;
  name: string;
  color: string;
}

/** What a seeder writes into an empty room, and the version it was begun from. */
export interface SeedContent {
  base: number;
  fill: (doc: Y.Doc) => void;
}

/** What the page sees of a session, replaced whole on every change. */
export interface CollabSnapshot {
  status: CollabStatus;
  room: string | null;
  doc: Y.Doc | null;
  awareness: Awareness | null;
  base: number;
  /** The room has content and the editor may show it. */
  ready: boolean;
  stopped: StopReason | null;
}

/** Keeps a room's document in the browser between reloads; null keeps nothing. */
export interface LocalStore {
  open: (room: string, doc: Y.Doc) => { destroy: () => Promise<void> | void; clear: () => Promise<void> | void };
}

export interface SessionOptions {
  url: string;
  user: CollabUser;
  /** Asked for the first content when the server picks this browser; afresh after a reset. */
  seed: (afresh: boolean) => Promise<SeedContent>;
  socket?: (url: string) => WebSocket;
  local?: LocalStore | null;
  /** Whether the browser says it is online; tests answer for it. */
  online?: () => boolean;
}

/** Marks what came from the server, so it is not sent back. */
const REMOTE = Symbol("remote");
/** Marks the seed, which goes to the server as a seed rather than an update. */
const SEEDING = Symbol("seeding");

/**
 * A browser's side of a page's shared draft: one Yjs document per room,
 * loaded from the server, kept in step both ways while connected, and
 * merged with what was written offline when the connection returns.
 */
export class CollabSession {
  private opts: SessionOptions;
  private ws: WebSocket | null = null;
  private snapshot: CollabSnapshot = { status: "connecting", room: null, doc: null, awareness: null, base: 0, ready: false, stopped: null };
  private listeners = new Set<() => void>();
  private local: ReturnType<LocalStore["open"]> | null = null;
  private loading = false;
  private loaded: Uint8Array[] = [];
  private lastLoad: Uint8Array[] = [];
  private seedAsked = false;
  private hadRoom = false;
  private attempt = 0;
  private retry: ReturnType<typeof setTimeout> | undefined;
  private seedWait: ReturnType<typeof setTimeout> | undefined;
  private destroyed = false;

  constructor(opts: SessionOptions) {
    this.opts = opts;
    window.addEventListener("online", this.onOnline);
    window.addEventListener("offline", this.onOffline);
    this.connect();
  }

  get state(): CollabSnapshot {
    return this.snapshot;
  }

  subscribe = (listener: () => void) => {
    this.listeners.add(listener);
    return () => this.listeners.delete(listener);
  };

  private set(change: Partial<CollabSnapshot>) {
    const next = { ...this.snapshot, ...change };
    next.ready = next.doc !== null && isSeeded(next.doc) && next.status !== "stopped";
    this.snapshot = next;
    for (const l of this.listeners) l();
  }

  private online() {
    return this.opts.online ? this.opts.online() : navigator.onLine;
  }

  private connect() {
    if (this.destroyed || this.snapshot.status === "stopped") return;
    clearTimeout(this.retry);
    if (!this.online()) {
      this.set({ status: "offline" });
      return;
    }
    const ws = (this.opts.socket ?? ((url) => new WebSocket(url)))(this.opts.url);
    ws.binaryType = "arraybuffer";
    this.ws = ws;
    this.set({ status: this.snapshot.doc ? "offline" : "connecting" });
    ws.onmessage = (event) => {
      if (this.ws === ws) this.receive(new Uint8Array(event.data as ArrayBuffer));
    };
    ws.onclose = (event) => {
      if (this.ws === ws) this.closed(event.code);
    };
  }

  private send(frame: Uint8Array) {
    if (this.ws?.readyState === WebSocket.OPEN) this.ws.send(frame);
  }

  private receive(frame: Uint8Array) {
    const m = decodeServer(frame);
    const doc = this.snapshot.doc;
    switch (m.type) {
      case "room":
        this.enter(m.room, m.base, m.seed);
        break;
      case "update":
        if (!doc) return;
        Y.applyUpdate(doc, m.update, REMOTE);
        if (this.loading) this.loaded.push(m.update);
        break;
      case "loaded":
        void this.finishLoad();
        break;
      case "awareness":
        if (this.snapshot.awareness) applyAwarenessUpdate(this.snapshot.awareness, m.update, REMOTE);
        break;
      case "base":
        this.set({ base: m.base });
        break;
      case "compact":
        if (this.lastLoad.length === m.count) this.send(encodeCompacted(m.from, m.to, m.count, Y.mergeUpdates(this.lastLoad)));
        this.lastLoad = [];
        break;
    }
  }

  /** A load begins: in the same room after a reconnect, or in a new one. */
  private enter(room: string, base: number, seed: boolean) {
    if (this.snapshot.room !== room) {
      this.leaveRoom(true);
      const doc = new Y.Doc();
      const awareness = new Awareness(doc);
      awareness.setLocalStateField("user", this.opts.user);
      doc.on("update", (update: Uint8Array, origin: unknown) => {
        if (origin !== REMOTE && origin !== SEEDING && !this.loading) this.send(encodeUpdate(update));
      });
      awareness.on("update", ({ added, updated, removed }: { added: number[]; updated: number[]; removed: number[] }, origin: unknown) => {
        if (origin === REMOTE) return;
        const changed = [...added, ...updated, ...removed];
        this.send(encodeAwareness(encodeAwarenessUpdate(awareness, changed)));
      });
      this.local = this.opts.local?.open(room, doc) ?? null;
      this.set({ room, doc, awareness });
    }
    this.loading = true;
    this.loaded = [];
    this.seedAsked = seed;
    this.set({ base });
  }

  /** The load is in: send what the server lacks, seed if asked, and go live. */
  private async finishLoad() {
    const doc = this.snapshot.doc;
    const awareness = this.snapshot.awareness;
    if (!doc || !awareness) return;
    const known = this.loaded.length > 0 ? Y.encodeStateVectorFromUpdate(Y.mergeUpdates(this.loaded)) : new Uint8Array([0]);
    this.lastLoad = this.loaded;
    this.loading = false;
    this.attempt = 0;
    // What was written offline, or what this browser kept from before.
    const missing = Y.encodeStateAsUpdate(doc, known);
    if (!isEmptyUpdate(missing) && isSeeded(doc)) this.send(encodeUpdate(missing));
    this.send(encodeAwareness(encodeAwarenessUpdate(awareness, [doc.clientID])));
    this.send(encodeQueryAwareness());
    this.set({ status: "live" });
    if (isSeeded(doc)) return;
    if (this.seedAsked) {
      const room = this.snapshot.room;
      const content = await this.opts.seed(this.hadRoom);
      if (this.snapshot.room !== room || this.snapshot.doc !== doc || isSeeded(doc)) return;
      doc.transact(() => {
        content.fill(doc);
        markSeeded(doc);
      }, SEEDING);
      this.send(encodeSeed(content.base, Y.encodeStateAsUpdate(doc)));
      this.set({ base: content.base });
      return;
    }
    // Somebody else was asked; if their content never comes, ask again.
    clearTimeout(this.seedWait);
    const waiting = doc;
    const onSeed = () => {
      if (isSeeded(waiting)) {
        clearTimeout(this.seedWait);
        waiting.off("update", onSeed);
        this.set({});
      }
    };
    waiting.on("update", onSeed);
    this.seedWait = setTimeout(() => {
      waiting.off("update", onSeed);
      if (this.snapshot.doc === waiting && !isSeeded(waiting)) this.ws?.close();
    }, COLLAB_SEED_WAIT_MS);
  }

  private closed(code: number) {
    this.ws = null;
    this.loading = false;
    const awareness = this.snapshot.awareness;
    if (awareness) {
      const others = [...awareness.getStates().keys()].filter((id) => id !== awareness.clientID);
      removeAwarenessStates(awareness, others, REMOTE);
    }
    if (code === CLOSE.signedOut || code === CLOSE.refused) {
      this.set({ status: "stopped", stopped: code === CLOSE.signedOut ? "signedOut" : "refused" });
      return;
    }
    if (code === CLOSE.gone) {
      // The room is gone; the next load names its successor.
      this.leaveRoom(false);
      this.set({ room: null, doc: null, awareness: null });
    }
    if (this.destroyed) return;
    this.set({ status: this.snapshot.doc ? "offline" : "connecting" });
    const wait = code === CLOSE.gone ? 0 : Math.min(COLLAB_RECONNECT_MAX_MS, COLLAB_RECONNECT_MIN_MS * 2 ** this.attempt++);
    this.retry = setTimeout(() => this.connect(), wait);
  }

  /** Lets go of the room's document; what it kept locally goes too unless asked to keep it. */
  private leaveRoom(keepLocal: boolean) {
    clearTimeout(this.seedWait);
    if (this.snapshot.doc) this.hadRoom = true;
    const local = this.local;
    this.local = null;
    if (local) void (keepLocal ? local.destroy() : local.clear());
    this.snapshot.awareness?.destroy();
    this.snapshot.doc?.destroy();
  }

  private onOnline = () => {
    if (!this.ws) this.connect();
  };

  private onOffline = () => {
    this.ws?.close();
  };

  /** Tells everybody in the room that this browser published a version from it. */
  published(version: number) {
    this.send(encodePublished(version));
  }

  /** Throws the room away for everybody in it. */
  discard() {
    this.send(encodeDiscard());
  }

  destroy() {
    this.destroyed = true;
    clearTimeout(this.retry);
    window.removeEventListener("online", this.onOnline);
    window.removeEventListener("offline", this.onOffline);
    // Connected and loaded, everything local reached the server, so the
    // browser need not keep it.
    const synced = this.snapshot.status === "live" && !this.loading;
    const ws = this.ws;
    this.ws = null;
    ws?.close(1000);
    this.leaveRoom(!synced);
    this.listeners.clear();
  }
}
