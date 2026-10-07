import type { Page, TestInfo } from "@playwright/test";
import { must, type StatorApi } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { orgTest as test } from "../fixtures/org";
import { ONE_LOOK, openUntil } from "../fixtures/replica";
import { uniqueName } from "../fixtures/seed";
import { expectAccessible, scrollsSideways, startInScheme, type ColourScheme } from "../fixtures/shell";
import { createPage, createSpace, deleteSpace, uniqueKey } from "../fixtures/spaces";

const doc = (text: string) => ({ type: "doc" as const, content: [{ type: "paragraph", content: [{ type: "text", text }] }] });
const panelRows = (page: Page) => page.locator("[data-shortcut-row]");
const STATUS_URL = "https://status.example.com/now";

/** The sidebar's shortcuts, opening the drawer first where the sidebar is one. */
async function sidebarShortcuts(page: Page, testInfo: TestInfo) {
  if (testInfo.project.name === "mobile") {
    await page.locator('[data-action="drawer"]').click();
    await expect(page.locator('[role="dialog"][data-sidebar="drawer"]')).toBeVisible();
  }
  return page.getByRole("list", { name: "Shortcuts", exact: true });
}

/** Opens a space until its sidebar lists the shortcuts named. */
async function expectSidebar(page: Page, testInfo: TestInfo, key: string, names: string[]) {
  await openUntil(page, `/s/${key}`, async () => {
    const list = await sidebarShortcuts(page, testInfo);
    await expect(list.getByRole("listitem")).toHaveText(names, ONE_LOOK);
  });
}

test.describe("space shortcuts", { tag: ["@auth"] }, () => {
  const made: string[] = [];
  test.afterEach(async ({ api }) => {
    for (const key of made.splice(0)) await deleteSpace(api, key);
  });

  async function freshSpace(api: StatorApi, testInfo: TestInfo, label: string) {
    const key = uniqueKey(testInfo);
    made.push(key);
    const space = await createSpace(api, key, uniqueName(testInfo, label));
    const runbook = await createPage(api, space.homePageId, uniqueName(testInfo, "Runbook"), doc("What to do at night."));
    return { key, space, runbook };
  }

  test("alice pins a page and an address, orders them by keyboard, and bob follows them but cannot change them", async ({
    page,
    api,
    apiAs,
    pageAs,
  }, testInfo) => {
    const { key, runbook } = await freshSpace(api, testInfo, "Shortcuts");

    await page.goto(`/s/${key}/settings?tab=shortcuts`);
    await expect(page.getByRole("tab", { name: "Shortcuts" })).toHaveAttribute("aria-selected", "true");
    await expect(page.getByText("No shortcuts yet. Add a page or a web address below.")).toBeVisible();
    const pagePicker = page.getByLabel("Page", { exact: true });
    await expect(pagePicker).toBeEnabled();
    await pagePicker.selectOption(runbook.id);
    await page.getByRole("button", { name: "Add shortcut" }).click();
    await expect(page.getByText(`Added ${runbook.title}.`)).toBeVisible();

    await page.getByRole("button", { name: "A web address" }).click();
    await page.getByLabel("Address").fill("javascript:alert(document.cookie)");
    await page.getByLabel("Label").fill("Status");
    await page.getByRole("button", { name: "Add shortcut" }).click();
    await expect(page.getByText("An address starts with https:// or http://, such as https://example.com.")).toBeVisible();
    await page.getByLabel("Address").fill(STATUS_URL);
    await page.getByRole("button", { name: "Add shortcut" }).click();
    await expect(page.getByText("Added Status.")).toBeVisible();
    await expect(panelRows(page)).toHaveCount(2);

    const up = page.getByRole("button", { name: "Move Status up" });
    await up.focus();
    await page.keyboard.press("Enter");
    await expect(page.getByText("Status is now 1 of 2.")).toBeVisible();
    await expect(panelRows(page).first()).toHaveAttribute("data-shortcut-name", "Status");
    await expect(page.getByRole("button", { name: "Move Status down" })).toBeFocused();
    expect(await scrollsSideways(page)).toBe(false);

    const list = await sidebarShortcuts(page, testInfo);
    await expect(list.getByRole("listitem")).toHaveText(["Status(opens in a new tab)", runbook.title]);
    const external = list.getByRole("link", { name: /Status/ });
    await expect(external).toHaveAttribute("href", STATUS_URL);
    await expect(external).toHaveAttribute("target", "_blank");
    expect((await external.getAttribute("rel"))?.split(" ")).toEqual(expect.arrayContaining(["noopener", "noreferrer"]));
    // Above the page tree, in the same part of the sidebar.
    const nav = page.locator(`[data-space-nav="${key}"]`).last();
    const order = await nav.evaluate((root) => {
      const shortcuts = root.querySelector("[data-sidebar-shortcuts]");
      const tree = root.querySelector('[role="tree"]');
      return Boolean(shortcuts && tree && shortcuts.compareDocumentPosition(tree) & Node.DOCUMENT_POSITION_FOLLOWING);
    });
    expect(order).toBe(true);
    await list.getByRole("link", { name: runbook.title }).click();
    await expect(page.locator("main [data-page-title]")).toHaveText(runbook.title);

    const bob = await pageAs("bob");
    await expectSidebar(bob, testInfo, key, ["Status(opens in a new tab)", runbook.title]);
    await bob.goto(`/s/${key}/settings?tab=shortcuts`);
    await expect(bob.getByText("Only an administrator of this space can add, move or remove its shortcuts. Ask one of them.")).toBeVisible();
    await expect(panelRows(bob)).toHaveCount(2);
    await expect(bob.getByRole("button", { name: /^(Move|Remove) / })).toHaveCount(0);
    await expect(bob.getByRole("button", { name: "Add shortcut" })).toHaveCount(0);

    const bobApi = await apiAs("bob");
    const refused = await bobApi.POST("/spaces/{spaceKey}/shortcuts", { params: { path: { spaceKey: key } }, body: { url: "https://example.com" } });
    expect(refused.response.status).toBe(403);
    expect((refused.error as { error: { message: string } }).error.message).toBe(
      "Only an administrator of this space can change its shortcuts. Ask one of them to add, move or remove a shortcut.",
    );
  });

  test("a shortcut to a page bob may not open is hidden from him", async ({ page, api, pageAs }, testInfo) => {
    const { key, space, runbook } = await freshSpace(api, testInfo, "Hidden");
    const salaries = await createPage(api, space.homePageId, uniqueName(testInfo, "Salaries"), doc("Numbers."));
    const me = must(await api.GET("/auth/me")).user.id;
    must(await api.PUT("/pages/{pageID}/restrictions", { params: { path: { pageID: salaries.id } }, body: { view: [{ type: "user", id: me }], edit: [] } }));
    // Runbook's comes last, so the list bob waits for is one only the newest write gives him.
    for (const target of [salaries, runbook]) {
      must(await api.POST("/spaces/{spaceKey}/shortcuts", { params: { path: { spaceKey: key } }, body: { pageId: target.id } }));
    }

    await expectSidebar(page, testInfo, key, [salaries.title, runbook.title]);
    const bob = await pageAs("bob");
    await expectSidebar(bob, testInfo, key, [runbook.title]);
    await expect(bob.getByText(salaries.title)).toHaveCount(0);
  });

  for (const scheme of ["light", "dark"] as ColourScheme[]) {
    test(`shortcuts are added, moved and removed from the keyboard and are accessible in ${scheme}`, async ({ page, api }, testInfo) => {
      const { key, runbook } = await freshSpace(api, testInfo, `Axe ${scheme}`);
      must(await api.POST("/spaces/{spaceKey}/shortcuts", { params: { path: { spaceKey: key } }, body: { pageId: runbook.id } }));
      await startInScheme(page, scheme);

      await page.goto(`/s/${key}/settings?tab=shortcuts`);
      await expect(panelRows(page)).toHaveCount(1);
      await page.getByRole("button", { name: "A web address" }).focus();
      await page.keyboard.press("Enter");
      await page.getByLabel("Address").focus();
      await page.keyboard.type(STATUS_URL);
      await page.keyboard.press("Enter");
      await expect(page.getByText("Added status.example.com.")).toBeVisible();
      await expectAccessible(page);

      await page.getByRole("button", { name: "Move status.example.com up" }).focus();
      await page.keyboard.press("Enter");
      await expect(page.getByText("status.example.com is now 1 of 2.")).toBeVisible();
      await expect(page.getByRole("button", { name: "Move status.example.com down" })).toBeFocused();
      await page.getByRole("button", { name: `Remove ${runbook.title}` }).focus();
      await page.keyboard.press("Enter");
      await expect(page.getByText(`Removed ${runbook.title}.`)).toBeVisible();
      await expect(panelRows(page)).toHaveCount(1);
      await expect(page.locator(":focus")).toHaveAttribute("data-action", "remove-shortcut");

      const list = await sidebarShortcuts(page, testInfo);
      await expect(list.getByRole("listitem")).toHaveText(["status.example.com(opens in a new tab)"]);
      await expectAccessible(page);
    });
  }
});
