import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";
import { ARMATURE_LOOKUP_MAX_KEYS } from "@/config";
import { allowlist } from "@/test/allowlist";
import { ISSUE_KEY_PATTERN, TYPED_KEY, issueKeysOf, issueUrl, keyFromIssueUrl, lookupBatches, normalizeKey, projectOf } from "./issueKeys";

const BASE = "https://armature.example.com";

describe("issue keys", () => {
  it("take the shape the server's allowlist takes, and no other", () => {
    expect(allowlist.nodes.armatureIssue?.attrs?.key?.pattern).toBe(ISSUE_KEY_PATTERN.source);
    const goSource = readFileSync(resolve(process.cwd(), "../backend/internal/armature/armature.go"), "utf8");
    expect(goSource).toContain(`KeyPattern = \`${ISSUE_KEY_PATTERN.source}\``);
  });

  it("are upper case, with a project of 2 to 10 and a number that does not start with 0", () => {
    expect(normalizeKey("cp-12")).toBe("CP-12");
    expect(normalizeKey(" Sec-2 ")).toBe("SEC-2");
    expect(normalizeKey("ABCDEFGHIJ-1")).toBe("ABCDEFGHIJ-1");
    for (const raw of ["C-1", "1P-1", "CP-0", "CP-01", "CP1", "ABCDEFGHIJK-1", "CP-1234567890123456789", "", null, 12]) {
      expect(normalizeKey(raw), String(raw)).toBeNull();
    }
    expect(projectOf("AB12-7")).toBe("AB12");
  });

  it("are found as typed when a space or a sign follows, and not inside a word", () => {
    const typed = (text: string) => TYPED_KEY.exec(text)?.[1] ?? null;
    expect(typed("see CP-12 ")).toBe("CP-12");
    expect(typed("CP-12,")).toBe("CP-12");
    expect(typed("(CP-12)")).toBe("CP-12");
    expect(typed("fixed in SEC-2.")).toBe("SEC-2");
    expect(typed("UTF-8 ")).toBe("UTF-8");
    expect(typed("aCP-12 ")).toBeNull();
    expect(typed("CP-12")).toBeNull();
    expect(typed("CP-12x")).toBeNull();
    expect(typed("cp-12 ")).toBeNull();
    expect(typed("CP-012 ")).toBeNull();
  });

  it("are read from the issue addresses of the connected Armature only", () => {
    expect(keyFromIssueUrl(`${BASE}/issues/CP-12`, BASE)).toBe("CP-12");
    expect(keyFromIssueUrl(` ${BASE}/issues/cp-12/ `, BASE)).toBe("CP-12");
    expect(keyFromIssueUrl(`${BASE}/issues/CP-12?tab=comments#c-3`, BASE)).toBe("CP-12");
    expect(keyFromIssueUrl("HTTPS://ARMATURE.EXAMPLE.COM/issues/CP-12", BASE)).toBe("CP-12");
    expect(keyFromIssueUrl(`${BASE}/issues/CP-12`, `${BASE}/`)).toBe("CP-12");
    for (const text of [
      `${BASE}/issues/CP-12`.replace("https", "http"),
      "https://elsewhere.example.com/issues/CP-12",
      `${BASE}/projects/CP/issues/CP-12`,
      `${BASE}/issues/CP-12/comments`,
      `${BASE}/issues/UTF8`,
      `${BASE}/search?q=key%20%3D%20CP-12`,
      `see ${BASE}/issues/CP-12`,
      "https://user:pw@armature.example.com/issues/CP-12",
      "CP-12",
      "",
    ]) {
      expect(keyFromIssueUrl(text, BASE), text).toBeNull();
    }
    expect(keyFromIssueUrl(`${BASE}/issues/CP-12`, null)).toBeNull();
  });

  it("open where Armature opens issues", () => {
    expect(issueUrl(`${BASE}/`, "CP-12")).toBe(`${BASE}/issues/CP-12`);
  });

  it("are collected from a document once each, chips and blocks alike", () => {
    const doc = {
      type: "doc",
      content: [
        {
          type: "paragraph",
          content: [
            { type: "armatureIssue", attrs: { key: "CP-2" } },
            { type: "text", text: "CP-9 is text" },
          ],
        },
        { type: "armatureIssueBlock", attrs: { key: "SEC-1" } },
        {
          type: "table",
          content: [
            {
              type: "tableRow",
              content: [{ type: "tableCell", content: [{ type: "paragraph", content: [{ type: "armatureIssue", attrs: { key: "CP-2" } }] }] }],
            },
          ],
        },
        { type: "paragraph", content: [{ type: "armatureIssue", attrs: { key: "bogus" } }] },
      ],
    };
    expect(issueKeysOf(doc)).toEqual(["CP-2", "SEC-1"]);
    expect(issueKeysOf(null)).toEqual([]);
  });

  it("are asked for in sorted batches the API takes", () => {
    const keys = Array.from({ length: ARMATURE_LOOKUP_MAX_KEYS + 3 }, (_, i) => `CP-${i + 1}`);
    const batches = lookupBatches([...keys, "CP-1"]);
    expect(batches.map((batch) => batch.length)).toEqual([ARMATURE_LOOKUP_MAX_KEYS, 3]);
    expect(batches.flat()).toEqual([...keys].sort());
    expect(lookupBatches(["SEC-1", "CP-2"])).toEqual(lookupBatches(["CP-2", "SEC-1"]));
  });
});
