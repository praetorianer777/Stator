import { describe, expect, it } from "vitest";
import { ARMATURE_RETURN_MAX_LENGTH } from "@/config";
import { connectHref, safeReturn, tokensUrl } from "./connectLink";

describe("the way back from the token form", () => {
  it("keeps a path of this application with its query and anchor", () => {
    expect(safeReturn("/spaces/eng/plan")).toBe("/spaces/eng/plan");
    expect(safeReturn("/spaces/eng/plan?tab=issues#roadmap")).toBe("/spaces/eng/plan?tab=issues#roadmap");
    expect(safeReturn("/")).toBe("/");
  });

  it.each([
    ["another site by its address", "https://evil.example/pages"],
    ["another site without a scheme", "//evil.example/pages"],
    ["another site behind a backslash", "/\\evil.example"],
    ["a tab the browser folds away", "/\t/evil.example"],
    ["a line break", "/pages\n/x"],
    ["a script", "javascript:alert(1)"],
    ["a relative path", "pages/plan"],
    ["the profile itself", "/settings/profile"],
    ["the profile with a query", "/settings/profile?return=%2F"],
    ["nothing", ""],
    ["something other than text", 42],
    ["a path longer than allowed", `/${"a".repeat(ARMATURE_RETURN_MAX_LENGTH)}`],
  ])("drops %s", (_, value) => {
    expect(safeReturn(value)).toBeUndefined();
  });

  it("builds the hint's address from where it was followed, and leaves out a way back it does not trust", () => {
    expect(connectHref("/spaces/eng/plan?tab=issues")).toBe("/settings/profile?return=%2Fspaces%2Feng%2Fplan%3Ftab%3Dissues#armature");
    expect(connectHref("//evil.example")).toBe("/settings/profile#armature");
    expect(connectHref(undefined)).toBe("/settings/profile#armature");
  });

  it("opens Armature's tokens under the connected address, with or without a trailing slash", () => {
    expect(tokensUrl("https://armature.example.com")).toBe("https://armature.example.com/settings/tokens");
    expect(tokensUrl("https://armature.example.com/")).toBe("https://armature.example.com/settings/tokens");
  });
});
