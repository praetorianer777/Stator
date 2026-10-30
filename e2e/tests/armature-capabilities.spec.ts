import type { APIRequestContext, Page, TestInfo } from "@playwright/test";
import { must, type StatorApi } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { teamTest as test } from "../fixtures/team";
import { openShowing } from "../fixtures/replica";
import { uniqueName } from "../fixtures/seed";
import { expectAccessible } from "../fixtures/shell";
import { createPage, createSpace, deleteSpace, uniqueKey, type Space } from "../fixtures/spaces";
import { armatureURL } from "../fixtures/stack";

// Stator reads what the connected Armature offers at /meta (#409). The stub
// answers there as an older Armature when reached by the host below, which
// the api's backchannel maps to it as it maps the stub's own address; a host
// of its own keeps what the api learned of it from every other spec.
const patFor = (tenant: string, person: string) => `armature_pat_${tenant}_${person}`;

function olderArmature(): { base: string; host: string } {
  const url = new URL(armatureURL());
  url.hostname = "older-armature.localhost";
  return { base: url.origin, host: url.host };
}

async function answerMetaAs(request: APIRequestContext, body: { capabilities?: string[]; absent?: boolean } | null) {
  const target = `${armatureURL()}/_stub-meta/${olderArmature().host}`;
  const answer = body ? await request.fetch(target, { method: "PUT", data: body }) : await request.fetch(target, { method: "DELETE" });
  expect(answer.status()).toBe(204);
}

const WITHOUT_REPORTS = ["calendar@1", "issue-embed@1", "plan@1", "remote-link-relations@1", "remote-links@1", "theme-export@1", "webhooks@1"];
const shown = (page: Page) => page.locator("main [data-doc]");

test.describe("Armature capabilities", { tag: ["@auth"] }, () => {
  const made: string[] = [];

  test.afterEach(async ({ api, request }) => {
    for (const key of made.splice(0)) await deleteSpace(api, key);
    await api.DELETE("/armature/connection");
    await answerMetaAs(request, null);
  });

  async function connect(api: StatorApi, slug: string) {
    await api.DELETE("/armature/connection");
    must(await api.PUT("/armature/connection", { body: { baseUrl: olderArmature().base } }));
    must(await api.PUT("/armature/account/token", { body: { token: patFor(slug, "alice") } }));
  }

  async function freshSpace(api: StatorApi, testInfo: TestInfo, label: string): Promise<Space> {
    const key = uniqueKey(testInfo);
    made.push(key);
    return createSpace(api, key, uniqueName(testInfo, label));
  }

  test("an Armature without reports@1 turns charts off, said on the settings page and in the chart", async ({ page, api, request, team }, testInfo) => {
    await answerMetaAs(request, { capabilities: WITHOUT_REPORTS });
    await connect(api, team.label);

    await page.goto("/admin/armature");
    const offered = page.locator("[data-armature-capabilities]");
    await expect(offered).toHaveAttribute("data-armature-capabilities", "missing");
    await expect(offered.locator('[data-missing="charts"]')).toHaveText(
      "Stator turns off charts, because Armature does not offer reports@1. Update Armature to a release that does.",
    );
    await expect(offered.locator("[data-missing]")).toHaveCount(1);
    await expectAccessible(page);

    const space = await freshSpace(api, testInfo, "Capabilities");
    const report = await createPage(api, space.homePageId, "Status report", {
      type: "doc",
      content: [
        { type: "paragraph", content: [{ type: "text", text: "Where we are." }] },
        { type: "armatureChart", attrs: { project: "CP", query: "project = CP", chart: "pie", groupBy: "priority", days: 30 } },
        { type: "paragraph", content: [{ type: "armatureIssue", attrs: { key: "CP-1" } }] },
      ],
    });
    await openShowing(page, `/s/${space.key}/p/${report.id}/status-report`, "Where we are.");
    const chart = shown(page).locator('[data-armature-chart="pie"]');
    await expect(chart).toHaveAttribute("data-state", "outdated");
    await expect(chart).toContainText(
      "The connected Armature does not offer reports@1, which Stator needs for charts. Ask an administrator to update Armature.",
    );
    // issue-embed@1 is there, so the chip beside it still shows its issue.
    await expect(shown(page).locator('[data-armature-issue="CP-1"]')).toHaveAttribute("data-state", "issue");

    // Updating Armature shows on the settings page at once.
    await answerMetaAs(request, null);
    await page.goto("/admin/armature");
    await expect(page.locator("[data-armature-capabilities]")).toHaveAttribute("data-armature-capabilities", "all");
    await expect(page.getByText("Armature offers everything Stator uses.")).toBeVisible();
  });

  test("an Armature from before /meta is too old to say what it offers, and chips say it needs updating", async ({ page, api, request, team }, testInfo) => {
    await answerMetaAs(request, { absent: true });
    await connect(api, team.label);

    await page.goto("/admin/armature");
    await expect(page.getByText(/This Armature is too old to say what it offers, because it does not answer \/api\/v1\/meta\./)).toBeVisible();
    await expect(page.locator('[data-missing="issues"]')).toContainText("issue-embed@1");

    const space = await freshSpace(api, testInfo, "Old Armature");
    const notes = await createPage(api, space.homePageId, "Notes", {
      type: "doc",
      content: [
        {
          type: "paragraph",
          content: [
            { type: "text", text: "See " },
            { type: "armatureIssue", attrs: { key: "CP-1" } },
          ],
        },
      ],
    });
    await openShowing(page, `/s/${space.key}/p/${notes.id}/notes`, "See");
    const chip = shown(page).locator('[data-armature-issue="CP-1"]');
    await expect(chip).toHaveAttribute("data-state", "outdated");
    await expect(chip).toContainText("Needs a newer Armature");
    await expectAccessible(page);
  });
});
