import { describe, expect, it } from "vitest";
import { buildDetails, buildLabel } from "./build";

describe("the build label", () => {
  it("names a release by its tag", () => {
    expect(buildLabel({ version: "0.1.0", commit: "abc1234" })).toBe("v0.1.0");
    expect(buildLabel({ version: "1.0.0-rc.1" })).toBe("v1.0.0-rc.1");
  });

  it("names any other build by its version and commit", () => {
    expect(buildLabel({ version: "0.1.0+4.gabc1234", commit: "abc1234" })).toBe("v0.1.0+4 · abc1234");
    expect(buildLabel({ version: "0.1.0+dev", commit: "def5678" })).toBe("v0.1.0+dev · def5678");
  });

  it("finds the commit in the version when it was not stamped apart", () => {
    expect(buildLabel({ version: "0.1.0+4.gabc1234" })).toBe("v0.1.0+4 · abc1234");
  });

  it("shortens a full hash", () => {
    expect(buildLabel({ version: "0.1.0+dev", commit: "0123456789abcdef0123456789abcdef01234567" })).toBe("v0.1.0+dev · 0123456");
  });

  it("says what it knows when a build carries no git at all", () => {
    expect(buildLabel({ version: "0.1.0+dev" })).toBe("v0.1.0+dev");
    expect(buildLabel({ version: "dev" })).toBe("dev");
  });
});

describe("the build details", () => {
  it("give the version, the commit and when it was built, a line each", () => {
    const lines = buildDetails({ version: "0.1.0+4.gabc1234", commit: "abc1234", builtAt: "2026-10-14T09:00:00Z" }).split("\n");
    expect(lines[0]).toBe("Stator 0.1.0+4.gabc1234");
    expect(lines[1]).toBe("Commit abc1234");
    expect(lines[2]).toMatch(/^Built .*2026/);
  });

  it("leave out what the build does not know", () => {
    expect(buildDetails({ version: "0.1.0+dev", builtAt: "not a time" })).toBe("Stator 0.1.0+dev");
  });
});
