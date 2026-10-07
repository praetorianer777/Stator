import * as decoding from "lib0/decoding";
import * as encoding from "lib0/encoding";

/**
 * The messages of a page's shared draft, in y-protocols' framing. Sync and
 * awareness are y-protocols' numbers; the rest are Stator's, as the api's
 * internal/collab names them.
 */
export const MSG = {
  sync: 0,
  awareness: 1,
  queryAwareness: 3,
  room: 100,
  published: 101,
  seed: 102,
  discard: 103,
  compact: 104,
  compacted: 105,
  base: 107,
} as const;

export const SYNC = { step1: 0, step2: 1, update: 2 } as const;

/** Close codes past the standard ones, as the api sends them. */
export const CLOSE = {
  signedOut: 4401,
  refused: 4403,
  slow: 4408,
  gone: 4409,
} as const;

/** What the server sends, read. */
export type ServerMessage =
  | { type: "room"; room: string; base: number; seed: boolean }
  | { type: "update"; update: Uint8Array }
  | { type: "loaded" }
  | { type: "awareness"; update: Uint8Array }
  | { type: "base"; base: number }
  | { type: "compact"; from: number; to: number; count: number }
  | { type: "unknown" };

export function decodeServer(frame: Uint8Array): ServerMessage {
  const d = decoding.createDecoder(frame);
  const type = decoding.readVarUint(d);
  switch (type) {
    case MSG.sync: {
      const sub = decoding.readVarUint(d);
      const update = decoding.readVarUint8Array(d);
      // An empty step 2 is how a load ends; any other step 2 is updates too.
      if (sub === SYNC.step2 && isEmptyUpdate(update)) return { type: "loaded" };
      return sub === SYNC.step1 ? { type: "unknown" } : { type: "update", update };
    }
    case MSG.awareness:
      return { type: "awareness", update: decoding.readVarUint8Array(d) };
    case MSG.room: {
      const room = decoding.readVarString(d);
      const base = decoding.readVarUint(d);
      return { type: "room", room, base, seed: decoding.readVarUint(d) === 1 };
    }
    case MSG.base:
      return { type: "base", base: decoding.readVarUint(d) };
    case MSG.compact: {
      const from = decoding.readVarUint(d);
      const to = decoding.readVarUint(d);
      return { type: "compact", from, to, count: decoding.readVarUint(d) };
    }
  }
  return { type: "unknown" };
}

/** An update that changes nothing: no structs, no deletions. */
export function isEmptyUpdate(update: Uint8Array): boolean {
  return update.length === 2 && update[0] === 0 && update[1] === 0;
}

const frame = (write: (e: encoding.Encoder) => void) => encoding.encode(write);

export const encodeUpdate = (update: Uint8Array) =>
  frame((e) => {
    encoding.writeVarUint(e, MSG.sync);
    encoding.writeVarUint(e, SYNC.update);
    encoding.writeVarUint8Array(e, update);
  });

export const encodeAwareness = (update: Uint8Array) =>
  frame((e) => {
    encoding.writeVarUint(e, MSG.awareness);
    encoding.writeVarUint8Array(e, update);
  });

export const encodeSeed = (base: number, update: Uint8Array) =>
  frame((e) => {
    encoding.writeVarUint(e, MSG.seed);
    encoding.writeVarUint(e, base);
    encoding.writeVarUint8Array(e, update);
  });

export const encodePublished = (version: number) =>
  frame((e) => {
    encoding.writeVarUint(e, MSG.published);
    encoding.writeVarUint(e, version);
  });

export const encodeDiscard = () => frame((e) => encoding.writeVarUint(e, MSG.discard));

export const encodeQueryAwareness = () => frame((e) => encoding.writeVarUint(e, MSG.queryAwareness));

export const encodeCompacted = (from: number, to: number, count: number, merged: Uint8Array) =>
  frame((e) => {
    encoding.writeVarUint(e, MSG.compacted);
    encoding.writeVarUint(e, from);
    encoding.writeVarUint(e, to);
    encoding.writeVarUint(e, count);
    encoding.writeVarUint8Array(e, merged);
  });
