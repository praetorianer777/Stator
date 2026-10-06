import type { Locator, Page } from "@playwright/test";
import type { StatorApi } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { withDatabase } from "../fixtures/db";
import { orgTest as test } from "../fixtures/org";
import { ONE_LOOK, openUntil } from "../fixtures/replica";
import { uniqueName } from "../fixtures/seed";
import { expectAccessible, scrollsSideways, startInScheme, type ColourScheme } from "../fixtures/shell";
import { createSpace, deleteSpace, uniqueKey } from "../fixtures/spaces";

const AUDIT_PATH = "/settings/audit";
// web/src/config.ts AUDIT_PAGE_SIZE: the page holds this many, so more spill onto a second.
const PAGE_SIZE = 50;
const SPILL = 7;
// The filters are read apart from the rows, and may come from a replica further behind.
const FACET_PLANTED = '#audit-action option[value="space.updated"]';

// The changes are written straight into the record, a second apart: making
// sixty through the API would only be slower at saying the same thing.
/** A space alice made, and changes to it beyond one page of the log. */
async function busySpace(api: StatorApi, orgId: string, key: string, name: string): Promise<{ id: string; name: string }> {
  const space = await createSpace(api, key, name);
  const me = (await api.GET("/auth/me")).data!.user;
  await withDatabase(async (db) => {
    await db.query(
      `INSERT INTO audit_log (org_id, actor_user_id, action, target_type, target_id, data, created_at)
       SELECT $1, $2, 'space.updated', 'space', $3, jsonb_build_object('key', $4::text, 'name', $5::text), now() - make_interval(secs => n)
       FROM generate_series(1, $6::int) AS n`,
      [orgId, me.id, space.id, key, name, PAGE_SIZE + SPILL],
    );
  });
  return { id: space.id, name };
}

// A page of the log holds two buttons a row, so reaching the pager takes a
// hundred presses and more.
const MAX_TABS = 300;

/** Presses Tab until the control has focus, as a person working the page by keyboard would. */
async function tabTo(page: Page, control: Locator): Promise<void> {
  // Asked through a locator, each of the hundred checks queried the whole page
  // again, and under load the walk alone outlasted the test's time.
  const target = await control.elementHandle();
  try {
    for (let i = 0; i < MAX_TABS; i++) {
      if (await target.evaluate((el) => el === document.activeElement)) return;
      await page.keyboard.press("Tab");
    }
  } finally {
    await target.dispose();
  }
  await expect(control).toBeFocused();
}

// Earlier tests of the file planted the same action in the same organization,
// so only a row naming this space shows that its changes have arrived.
const plantedRow = (page: Page, name: string) => page.locator('[data-audit-row="space.updated"]').filter({ hasText: name }).first();

/** Opens the log and waits for the changes planted on the space named. */
async function openLog(page: Page, name: string): Promise<void> {
  await openUntil(page, AUDIT_PATH, async () => {
    await expect(plantedRow(page, name)).toBeVisible(ONE_LOOK);
    await expect(page.locator(FACET_PLANTED)).toBeAttached(ONE_LOOK);
  });
}

test.describe("the audit log", { tag: ["@auth"] }, () => {
  const made: string[] = [];
  test.afterEach(async ({ api }) => {
    for (const key of made.splice(0)) await deleteSpace(api, key);
  });

  test("an administrator opens it, narrows it to one space's changes and pages through them", async ({ page, api, freshOrg }, testInfo) => {
    const key = uniqueKey(testInfo);
    made.push(key);
    const space = await busySpace(api, freshOrg.id, key, uniqueName(testInfo, "Busy"));

    await openUntil(page, "/", async () => {
      await page.locator('[data-action="account"]').click();
      await page.locator('[role="menu"] [data-action="audit-log"]').click();
      await expect(page).toHaveURL(/\/settings\/audit$/);
      await expect(plantedRow(page, space.name)).toBeVisible(ONE_LOOK);
      await expect(page.locator(FACET_PLANTED)).toBeAttached(ONE_LOOK);
    });
    await expect(page.locator("main").getByRole("heading", { level: 1 })).toHaveText("Audit log");

    await page.getByLabel("Action").selectOption("space.updated");
    await expect(page.locator("[data-audit-row]").first()).toHaveAttribute("data-audit-row", "space.updated");
    await page
      .getByRole("button", { name: `Show only entries about ${space.name}` })
      .first()
      .click();
    await expect(page.locator("[data-audit-target-filter]")).toHaveText(new RegExp(`Only entries about ${space.name}`));
    await expect(page.locator("[data-audit-row]")).toHaveCount(PAGE_SIZE);
    await expect(page.locator("[data-audit-page]")).toHaveText("Page 1");

    await page.locator('[data-action="audit-older"]').click();
    await expect(page.locator("[data-audit-page]")).toHaveText("Page 2");
    await expect(page.locator("[data-audit-row]")).toHaveCount(SPILL);
    await expect(page.locator('[data-action="audit-older"]')).toBeDisabled();
    await page.locator('[data-action="audit-newer"]').click();
    await expect(page.locator("[data-audit-page]")).toHaveText("Page 1");
    await expect(page.locator("[data-audit-row]")).toHaveCount(PAGE_SIZE);

    const href = await page.locator('[data-action="export-audit"]').getAttribute("href");
    expect(href).toContain(`target=${space.id}`);
    const csv = await page.request.get(href!);
    expect(csv.status()).toBe(200);
    const lines = (await csv.text()).trim().split("\n");
    expect(lines).toHaveLength(PAGE_SIZE + SPILL + 1);
    expect(lines[0]).toBe("time,action,actor_id,actor,target_type,target_id,ip,data");

    await expect(async () => {
      const exported = (await api.GET("/audit", { params: { query: { action: "audit.exported" } } })).data!.entries;
      expect(exported.some((entry) => (entry.data as { targetId?: string }).targetId === space.id)).toBe(true);
    }).toPass();

    await page.locator('[data-action="clear-audit-filters"]').click();
    await expect(page.getByLabel("Action")).toHaveValue("");
    await expect(page.locator("[data-audit-target-filter]")).toHaveCount(0);
  });

  test("bob, a member, is refused it", async ({ apiAs, pageAs }) => {
    const bobApi = await apiAs("bob");
    const refused = await bobApi.GET("/audit");
    expect(refused.response.status).toBe(403);
    expect((refused.error as { error?: { message?: string } } | undefined)?.error?.message).toMatch(
      /^Only an administrator of this organization can do that\. Ask one of them/,
    );

    const bob = await pageAs("bob");
    await bob.goto(AUDIT_PATH);
    await expect(bob.locator("[data-audit-refused]")).toContainText("Only an administrator of this organization reads its audit log.");
    await expect(bob.locator("[data-audit-row]")).toHaveCount(0);
    await bob.locator('[data-action="account"]').click();
    await expect(bob.getByRole("menu")).toBeVisible();
    await expect(bob.locator('[role="menu"] [data-action="audit-log"]')).toHaveCount(0);
  });

  test("it is narrowed and paged from the keyboard", { tag: ["@desktop"] }, async ({ page, api, freshOrg }, testInfo) => {
    const key = uniqueKey(testInfo);
    made.push(key);
    const space = await busySpace(api, freshOrg.id, key, uniqueName(testInfo, "Keys"));
    await openLog(page, space.name);

    await page.getByLabel("Action").focus();
    await tabTo(page, page.getByRole("button", { name: `Show only entries about ${space.name}` }).first());
    await page.keyboard.press("Enter");
    await expect(page.locator("[data-audit-target-filter]")).toBeVisible();
    await expect(page.locator("[data-audit-row]")).toHaveCount(PAGE_SIZE);

    await tabTo(page, page.locator('[data-action="audit-older"]'));
    await page.keyboard.press("Enter");
    await expect(page.locator("[data-audit-page]")).toHaveText("Page 2");
  });

  for (const scheme of ["light", "dark"] as ColourScheme[]) {
    test(`it passes axe in ${scheme}`, async ({ page, api, freshOrg }, testInfo) => {
      const key = uniqueKey(testInfo);
      made.push(key);
      const space = await busySpace(api, freshOrg.id, key, uniqueName(testInfo, `Axe ${scheme}`));
      await startInScheme(page, scheme);
      await openLog(page, space.name);
      await expectAccessible(page);
    });
  }

  test("on a phone the filters stack and the page never scrolls sideways", { tag: ["@mobile"] }, async ({ page, api, freshOrg }, testInfo) => {
    const key = uniqueKey(testInfo);
    made.push(key);
    const space = await busySpace(api, freshOrg.id, key, uniqueName(testInfo, "Phone"));
    await openLog(page, space.name);
    await expect(page.getByLabel("Action")).toBeVisible();
    await expect(page.getByLabel("From")).toBeVisible();
    await expect(page.locator('[data-action="export-audit"]')).toBeVisible();
    expect(await scrollsSideways(page)).toBe(false);
    await page.getByLabel("Action").selectOption("space.updated");
    await expect(page.locator("[data-audit-row]").first()).toHaveAttribute("data-audit-row", "space.updated");
    expect(await scrollsSideways(page)).toBe(false);
  });
});
