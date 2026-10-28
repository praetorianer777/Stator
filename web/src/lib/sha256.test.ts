import { createHash } from "node:crypto";
import { describe, expect, it } from "vitest";
import { sha256Hex } from "./sha256";

describe("sha256Hex", () => {
  it("hashes as the api does, across block boundaries and beyond ASCII", () => {
    // The hash the api's diagram test pins for the same text.
    expect(sha256Hex("plantuml\na -> b")).toBe("c0a8ecc53134aa4a7f7c3755bae5026c7659c435622e5d0f19b042c0f9802886");
    for (const text of ["", "abc", "x".repeat(55), "x".repeat(56), "x".repeat(64), "x".repeat(119), "ä → 🦘\n".repeat(500)]) {
      expect(sha256Hex(text)).toBe(createHash("sha256").update(text, "utf8").digest("hex"));
    }
  });
});
