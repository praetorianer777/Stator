import { describe, expect, it } from "vitest";
import { decodeServer, encodeAwareness, encodeCompacted, encodeDiscard, encodePublished, encodeSeed, encodeUpdate } from "./protocol";

const hex = (bytes: Uint8Array) => Array.from(bytes, (b) => b.toString(16).padStart(2, "0")).join("");
const bytes = (text: string) => new Uint8Array(text.match(/../g)?.map((h) => Number.parseInt(h, 16)) ?? []);

// The same bytes are in backend/internal/collab/collab_test.go, which holds
// the server to them.
describe("the shared draft's messages", () => {
  it("are sent as the server reads them", () => {
    expect(hex(encodeUpdate(new Uint8Array([1, 2, 3])))).toBe("000203010203");
    expect(hex(encodeSeed(4, new Uint8Array([1, 0xff])))).toBe("66040201ff");
    expect(hex(encodePublished(5))).toBe("6505");
    expect(hex(encodeDiscard())).toBe("67");
    expect(hex(encodeCompacted(1, 200, 2, new Uint8Array([9, 9])))).toBe("6901c80102020909");
    expect(hex(encodeAwareness(new Uint8Array([0, 0, 0])))).toBe("0103000000");
  });

  it("are read as the server sends them", () => {
    const room = "0195f000-0000-7000-8000-0000000000a1";
    const roomHex = hex(new TextEncoder().encode(room));
    expect(decodeServer(bytes(`6424${roomHex}ac0201`))).toEqual({ type: "room", room, base: 300, seed: true });
    expect(decodeServer(bytes("000203010203"))).toEqual({ type: "update", update: new Uint8Array([1, 2, 3]) });
    expect(decodeServer(bytes("0001020000"))).toEqual({ type: "loaded" });
    expect(decodeServer(bytes("6b07"))).toEqual({ type: "base", base: 7 });
    expect(decodeServer(bytes("6801c801c801"))).toEqual({ type: "compact", from: 1, to: 200, count: 200 });
    expect(decodeServer(bytes("0106010502027b7d"))).toEqual({ type: "awareness", update: bytes("010502027b7d") });
    expect(decodeServer(bytes("63"))).toEqual({ type: "unknown" });
  });
});
