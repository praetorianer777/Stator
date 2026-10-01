import type { APIRequestContext, Page } from "@playwright/test";
import { must } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { orgTest as test } from "../fixtures/org";
import { uniqueName } from "../fixtures/seed";
import { createPage, createSpace, deleteSpace, uniqueKey, type Page as WikiPage } from "../fixtures/spaces";
import { apiFromStubURL, armatureURL } from "../fixtures/stack";

// The stub signs and sends the webhook exactly as Armature does, to the api as
// the stub reaches it, so the delivery goes through the real receiver.
const patFor = (tenant: string, person: string) => `armature_pat_${tenant}_${person}`;
const SECRET = "armature_whs_abcdefghijklmnopqrstuvwxyz0123456789ABCDEFG";

const heading = (page: Page) => page.locator("main").getByRole("heading", { level: 1 });

async function stub(request: APIRequestContext, method: "POST" | "PATCH" | "DELETE", path: string, data?: unknown) {
  const answer = await request.fetch(`${armatureURL()}/_stub/${path}`, { method, data });
  expect(answer.ok(), `the stub answered ${method} ${path} with ${answer.status()}`).toBe(true);
  return answer.status() === 204 ? null : ((await answer.json()) as Record<string, unknown>);
}

async function openPage(page: Page, spaceKey: string, target: WikiPage) {
  await expect(async () => {
    await page.goto(`/s/${spaceKey}/p/${target.id}/page`);
    await expect(heading(page)).toHaveText(target.title, { timeout: 1_000 });
  }).toPass();
}

test.describe("Armature webhooks", { tag: ["@auth"] }, () => {
  let spaceKey = "";

  test.beforeEach(async ({ api, freshOrg, request }) => {
    await stub(request, "DELETE", freshOrg.slug);
    await api.DELETE("/armature/connection");
    must(await api.PUT("/armature/connection", { body: { baseUrl: armatureURL(), orgSlug: freshOrg.slug, webhookSecret: SECRET } }));
    must(await api.PUT("/armature/account/token", { body: { token: patFor(freshOrg.slug, "alice") } }));
  });

  test.afterEach(async ({ api }) => {
    if (spaceKey) await deleteSpace(api, spaceKey);
    await api.DELETE("/armature/connection");
  });

  test("alice sees an issue's new status as soon as Armature announces it", async ({ page, api, freshOrg, request }, testInfo) => {
    spaceKey = uniqueKey(testInfo);
    const space = await createSpace(api, spaceKey, uniqueName(testInfo, "Hooks"));
    const target = await createPage(api, space.homePageId, uniqueName(testInfo, "Status"), {
      type: "doc",
      content: [
        {
          type: "paragraph",
          content: [
            { type: "text", text: "Tracked in " },
            { type: "armatureIssue", attrs: { key: "CP-3" } },
          ],
        },
      ],
    } as Parameters<typeof createPage>[3]);

    await openPage(page, space.key, target);
    const status = page.locator('[data-doc] [data-armature-issue="CP-3"] [data-status-category]');
    await expect(status).toHaveAttribute("data-status-category", "todo");

    await stub(request, "PATCH", `${freshOrg.slug}/issues/CP-3`, { statusCategory: "done" });
    // Within the cache's minute the page still shows what was cached.
    await page.reload();
    await expect(status).toHaveAttribute("data-status-category", "todo");

    const sent = await stub(request, "POST", `${freshOrg.slug}/webhooks`, {
      url: `${apiFromStubURL()}/api/v1/armature/webhook/${freshOrg.slug}`,
      secret: SECRET,
      topic: "issue.transitioned",
      payload: { key: "CP-3" },
    });
    expect(sent?.status).toBe(204);

    await page.reload();
    await expect(status).toHaveAttribute("data-status-category", "done");

    // A delivery signed with another secret is refused by the receiver.
    const forged = await stub(request, "POST", `${freshOrg.slug}/webhooks`, {
      url: `${apiFromStubURL()}/api/v1/armature/webhook/${freshOrg.slug}`,
      secret: `${SECRET.slice(0, -1)}H`,
      topic: "issue.transitioned",
      payload: { key: "CP-3" },
    });
    expect(forged?.status).toBe(401);
  });
});
