import type { APIRequestContext, TestInfo } from "@playwright/test";
import { must, type StatorApi } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { caretTo } from "../fixtures/editor";
import { orgTest as test } from "../fixtures/org";
import { openPage } from "../fixtures/replica";
import { uniqueName } from "../fixtures/seed";
import { expectAccessible, startInScheme, type ColourScheme } from "../fixtures/shell";
import { createPage, createSpace, deleteSpace, publishFromEditor, uniqueKey } from "../fixtures/spaces";
import { armatureURL, WEB_URL } from "../fixtures/stack";

// The stub makes a person of any name on first use, in the Armature
// organization its token names; alice acts as Armature's admin here.
const patFor = (tenant: string, person: string) => `armature_pat_${tenant}_${person}`;
const SCHEMES: ColourScheme[] = ["light", "dark"];

type Body = Parameters<typeof createPage>[3];
const withChip = (key: string): Body =>
  ({
    type: "doc",
    content: [
      {
        type: "paragraph",
        content: [
          { type: "text", text: "Tracked in " },
          { type: "armatureIssue", attrs: { key } },
        ],
      },
    ],
  }) as Body;

type StubLink = { issueKey: string; url: string; title: string; source: string; createdByName: string };

/** The remote links the stub holds for the organization's tenant on one page. */
async function linksIn(request: APIRequestContext, tenant: string, url: string): Promise<StubLink[]> {
  const answer = await request.get(`${armatureURL()}/_stub/${tenant}/remote-links`);
  expect(answer.ok(), `the stub answered ${answer.status()}`).toBe(true);
  const { remoteLinks } = (await answer.json()) as { remoteLinks: StubLink[] };
  return remoteLinks.filter((each) => each.url === url);
}

test.describe("pages linked in Armature", { tag: ["@auth"] }, () => {
  const made: string[] = [];

  test.beforeEach(async ({ api, freshOrg, request }) => {
    await request.delete(`${armatureURL()}/_stub/${freshOrg.slug}`);
    await api.DELETE("/armature/connection");
    must(await api.PUT("/armature/connection", { body: { baseUrl: armatureURL(), orgSlug: freshOrg.slug } }));
    must(await api.PUT("/armature/account/token", { body: { token: patFor(freshOrg.slug, "admin") } }));
  });

  test.afterEach(async ({ api }) => {
    for (const key of made.splice(0)) await deleteSpace(api, key);
    await api.DELETE("/armature/connection");
  });

  async function freshSpace(api: StatorApi, testInfo: TestInfo) {
    const key = uniqueKey(testInfo);
    made.push(key);
    return createSpace(api, key, uniqueName(testInfo, "Runbooks"));
  }

  for (const scheme of SCHEMES) {
    test(`a published chip links the page on its issue, and taking it out unlinks it, in ${scheme}`, async ({ page, api, freshOrg, request }, testInfo) => {
      await startInScheme(page, scheme);
      const space = await freshSpace(api, testInfo);
      const target = await createPage(api, space.homePageId, uniqueName(testInfo, "Key rotation"), withChip("CP-4"));
      const url = `${WEB_URL}/s/${space.key}/p/${target.id}`;

      await openPage(page, space.key, target);
      const section = page.locator("[data-armature-links]");
      await expect(section.getByRole("heading", { name: "Linked in Armature" })).toBeVisible();
      const row = section.locator('[data-armature-link="CP-4"]');
      await expect(row).toHaveAttribute("data-state", "synced");
      await expect(row.getByRole("link", { name: "CP-4" })).toHaveAttribute("href", `${armatureURL()}/issues/CP-4`);
      await expect(row).toContainText("Linked");
      await expectAccessible(page);

      const linked = await linksIn(request, freshOrg.slug, url);
      expect(linked.map((each) => [each.issueKey, each.title, each.source, each.createdByName])).toEqual([["CP-4", target.title, "Stator", "Admin"]]);

      await page.locator('[data-action="edit-page"]').click();
      const box = page.locator("#page-body");
      await expect(box.locator('[data-armature-issue="CP-4"]')).toBeVisible();
      await caretTo(box, "end");
      await page.keyboard.press("Backspace");
      await expect(box.locator('[data-armature-issue="CP-4"]')).toHaveCount(0);
      await publishFromEditor(page);

      await expect(section).toBeHidden();
      await expect.poll(async () => (await linksIn(request, freshOrg.slug, url)).length).toBe(0);
      await expectAccessible(page);
    });
  }
});
