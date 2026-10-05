import * as decoding from "lib0/decoding";
import * as encoding from "lib0/encoding";
import * as Y from "yjs";
import { CLOSE, MSG, SYNC } from "@/features/collab/protocol";

const frame = (write: (e: encoding.Encoder) => void) => encoding.encode(write);
const updateFrame = (u: Uint8Array) =>
  frame((e) => {
    encoding.writeVarUint(e, MSG.sync);
    encoding.writeVarUint(e, SYNC.update);
    encoding.writeVarUint8Array(e, u);
  });
const loadedFrame = () =>
  frame((e) => {
    encoding.writeVarUint(e, MSG.sync);
    encoding.writeVarUint(e, SYNC.step2);
    encoding.writeVarUint8Array(e, new Uint8Array([0, 0]));
  });
const baseFrame = (base: number) =>
  frame((e) => {
    encoding.writeVarUint(e, MSG.base);
    encoding.writeVarUint(e, base);
  });

/** A socket to the stand-in server, as the browser's WebSocket behaves. */
export class FakeSocket {
  static readonly OPEN = 1;
  readyState = 0;
  binaryType = "blob";
  onmessage: ((event: { data: ArrayBuffer }) => void) | null = null;
  onclose: ((event: { code: number }) => void) | null = null;
  constructor(private server: FakeCollabServer) {}

  send(data: Uint8Array) {
    if (this.readyState !== FakeSocket.OPEN) return;
    this.server.receive(this, data);
  }

  close(code = 1005) {
    if (this.readyState === 3) return;
    this.readyState = 3;
    this.server.leave(this);
    queueMicrotask(() => this.onclose?.({ code }));
  }

  deliver(data: Uint8Array) {
    if (this.readyState !== FakeSocket.OPEN) return;
    const copy = data.slice().buffer;
    queueMicrotask(() => {
      if (this.readyState === FakeSocket.OPEN) this.onmessage?.({ data: copy });
    });
  }
}

/**
 * The api's side of a shared draft in memory: a room of stored updates,
 * passed between the sockets as handleCollab and the hub pass them.
 */
export class FakeCollabServer {
  room = "room-1";
  base: number;
  seeded = false;
  seedGranted = false;
  updates: Uint8Array[] = [];
  sockets = new Set<FakeSocket>();
  /** The last awareness each socket sent, which a newcomer is given. */
  awareness = new Map<FakeSocket, Uint8Array>();
  /** Asks for a merge once a load carries this many updates. */
  compactAt = Number.POSITIVE_INFINITY;
  compactions: Uint8Array[] = [];
  published: number[] = [];
  private rooms = 1;

  constructor(base = 1) {
    this.base = base;
  }

  socket = (_url: string): WebSocket => {
    const s = new FakeSocket(this);
    queueMicrotask(() => this.join(s));
    return s as unknown as WebSocket;
  };

  private join(s: FakeSocket) {
    s.readyState = FakeSocket.OPEN;
    this.sockets.add(s);
    const seed = !this.seeded && !this.seedGranted;
    if (seed) this.seedGranted = true;
    s.deliver(
      frame((e) => {
        encoding.writeVarUint(e, MSG.room);
        encoding.writeVarString(e, this.room);
        encoding.writeVarUint(e, this.base);
        encoding.writeVarUint(e, seed ? 1 : 0);
      }),
    );
    for (const u of this.updates) s.deliver(updateFrame(u));
    s.deliver(loadedFrame());
    for (const [other, said] of this.awareness) if (other !== s) s.deliver(said);
    if (this.updates.length >= this.compactAt) {
      s.deliver(
        frame((e) => {
          encoding.writeVarUint(e, MSG.compact);
          encoding.writeVarUint(e, 1);
          encoding.writeVarUint(e, this.updates.length);
          encoding.writeVarUint(e, this.updates.length);
        }),
      );
    }
  }

  leave(s: FakeSocket) {
    this.sockets.delete(s);
    this.awareness.delete(s);
  }

  private others(from: FakeSocket | null, data: Uint8Array) {
    for (const s of this.sockets) if (s !== from) s.deliver(data);
  }

  receive(from: FakeSocket, data: Uint8Array) {
    const d = decoding.createDecoder(data);
    const type = decoding.readVarUint(d);
    switch (type) {
      case MSG.sync: {
        const sub = decoding.readVarUint(d);
        const update = decoding.readVarUint8Array(d);
        if (sub === SYNC.step1) return;
        this.updates.push(update);
        this.others(from, updateFrame(update));
        return;
      }
      case MSG.awareness:
        this.awareness.set(from, data);
        this.others(from, data);
        return;
      case MSG.queryAwareness:
        for (const [other, said] of this.awareness) if (other !== from) from.deliver(said);
        return;
      case MSG.seed: {
        const base = decoding.readVarUint(d);
        const update = decoding.readVarUint8Array(d);
        if (this.seeded) {
          from.close(CLOSE.gone);
          return;
        }
        this.seeded = true;
        this.base = base;
        this.updates.push(update);
        this.others(from, updateFrame(update));
        this.others(null, baseFrame(base));
        return;
      }
      case MSG.published: {
        const version = decoding.readVarUint(d);
        this.published.push(version);
        this.base = version;
        this.others(null, baseFrame(version));
        return;
      }
      case MSG.discard:
        this.reset();
        return;
      case MSG.compacted: {
        decoding.readVarUint(d);
        decoding.readVarUint(d);
        const count = decoding.readVarUint(d);
        const merged = decoding.readVarUint8Array(d);
        this.compactions.push(merged);
        this.updates = [merged, ...this.updates.slice(count)];
        return;
      }
    }
  }

  /** Starts the room afresh, as a discard or a publish from elsewhere does. */
  reset() {
    this.room = `room-${++this.rooms}`;
    this.seeded = false;
    this.seedGranted = false;
    this.updates = [];
    for (const s of [...this.sockets]) s.close(CLOSE.gone);
  }

  /** Lets a socket go with a code, as the server does. */
  drop(code: number) {
    for (const s of [...this.sockets]) s.close(code);
  }

  /** The room's document as the server's updates make it. */
  doc(): Y.Doc {
    const doc = new Y.Doc();
    for (const u of this.updates) Y.applyUpdate(doc, u);
    return doc;
  }
}
