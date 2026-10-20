import type { Page, TestInfo } from "@playwright/test";
import type { StatorApi } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { teamTest as test } from "../fixtures/team";
import { openWithout } from "../fixtures/replica";
import { uniqueName } from "../fixtures/seed";
import { expectAccessible } from "../fixtures/shell";
import { createPage, createSpace, deleteSpace, uniqueKey, type Page as WikiPage } from "../fixtures/spaces";

const panel = (page: Page) => page.locator("[data-attachments]");
const row = (page: Page, name: string) => panel(page).locator(`[data-attachment="${name}"]`);
const notice = (page: Page) => page.locator("[data-attachment-notice]");
const csv = (body: string) => ({ name: "budget.csv", mimeType: "text/csv", buffer: Buffer.from(body) });

async function textOf(page: Page, href: string | null): Promise<string> {
  return (await page.request.get(href!)).text();
}

test.describe("file versions", { tag: ["@auth"] }, () => {
  const made: string[] = [];
  test.afterEach(async ({ api }) => {
    for (const key of made.splice(0)) await deleteSpace(api, key);
  });

  async function aPage(api: StatorApi, testInfo: TestInfo): Promise<{ key: string; page: WikiPage }> {
    const key = uniqueKey(testInfo);
    made.push(key);
    const space = await createSpace(api, key, uniqueName(testInfo, "Versions"));
    return {
      key,
      page: await createPage(api, space.homePageId, "Budget", {
        type: "doc",
        content: [{ type: "paragraph", content: [{ type: "text", text: "Numbers." }] }, { type: "attachmentList" }],
      }),
    };
  }

  test("an author uploads a file again, restores the first version as the latest, and deletes a version and then the file", async ({ page, api }, testInfo) => {
    test.slow();
    const { key, page: wiki } = await aPage(api, testInfo);
    const path = `/s/${key}/p/${wiki.id}/budget`;
    await page.goto(path);
    await expect(panel(page)).toContainText("Nothing attached yet.");

    const input = panel(page).locator("[data-attachment-input]");
    await input.setInputFiles(csv("q1,10\n"));
    await expect(row(page, "budget.csv")).toHaveAttribute("data-version", "1");
    await input.setInputFiles(csv("q1,10\nq2,12\n"));
    await expect(row(page, "budget.csv")).toHaveAttribute("data-version", "2");
    await expect(panel(page).locator("[data-attachment]")).toHaveCount(1);
    await expect(panel(page).getByRole("heading")).toHaveText("Attachments (1)");

    const budget = row(page, "budget.csv");
    await budget.getByText("1 earlier version").click();
    await budget.getByRole("button", { name: "Restore budget.csv, version 1" }).click();
    await expect(notice(page)).toHaveText("Restored version 1 of budget.csv as version 3.");
    await expect(budget).toHaveAttribute("data-version", "3");
    await expect(budget).toContainText("Version 3, restored from version 1");
    expect(await textOf(page, await budget.locator('[data-action="download-attachment"]').first().getAttribute("href"))).toBe("q1,10\n");

    // The files block in the page reads the same versions.
    const block = page.locator("main [data-doc]").getByRole("region", { name: "Files on this page" });
    await expect(block.locator('[data-listed-file="budget.csv"]')).toHaveAttribute("data-version", "3");
    await expect(block).toContainText("Version 3, restored from version 1");

    // Both earlier versions stay, the one restored among them.
    await budget.getByText("2 earlier versions").click();
    const first = budget.locator('[data-attachment-version="1"]');
    const second = budget.locator('[data-attachment-version="2"]');
    expect(await textOf(page, await first.locator('[data-action="download-attachment"]').getAttribute("href"))).toBe("q1,10\n");
    expect(await textOf(page, await second.locator('[data-action="download-attachment"]').getAttribute("href"))).toBe("q1,10\nq2,12\n");
    await expectAccessible(page);

    page.once("dialog", (dialog) => void dialog.accept());
    await first.getByRole("button", { name: "Delete budget.csv, version 1" }).click();
    await expect(notice(page)).toHaveText("Deleted version 1 of budget.csv.");
    await expect(budget.getByText("1 earlier version")).toBeVisible();
    await expect(budget).toHaveAttribute("data-version", "3");

    const asked: string[] = [];
    page.once("dialog", (dialog) => {
      asked.push(dialog.message());
      void dialog.accept();
    });
    await budget.locator('[data-action="delete-attachment"]').click();
    await expect(notice(page)).toHaveText("Deleted budget.csv.");
    await expect(row(page, "budget.csv")).toHaveCount(0);
    expect(asked[0]).toContain("Delete budget.csv and all 2 of its versions for good?");
    const left = (await api.GET("/pages/{pageID}/attachments", { params: { path: { pageID: wiki.id } } })).data?.attachments;
    expect(left).toEqual([]);
  });

  // The list's first load can be answered before an upload is stored and
  // reach the page after it (#327): the file must show all the same.
  test("a file uploaded while the list is still loading appears", async ({ page, api }, testInfo) => {
    const { key, page: wiki } = await aPage(api, testInfo);
    let late = true;
    let release = () => {};
    const released = new Promise<void>((resolve) => (release = resolve));
    let fulfilled: Promise<void> = Promise.resolve();
    await page.route(`**/api/v1/pages/${wiki.id}/attachments`, (route) => {
      if (route.request().method() !== "GET" || !late) return route.continue();
      late = false;
      // The handler must not outlive the test: a response fetched here is
      // disposed once the test ends, and fulfilling it then throws.
      fulfilled = (async () => {
        const answeredBefore = await route.fetch();
        await released;
        await route.fulfill({ response: answeredBefore });
      })();
    });
    await page.goto(`/s/${key}/p/${wiki.id}/budget`);
    await expect(row(page, "budget.csv")).toHaveCount(0);
    await panel(page).locator("[data-attachment-input]").setInputFiles(csv("one\n"));
    await expect(page.locator("[data-attachment-notice]")).toHaveText("Attached budget.csv.");
    const arrived = page.waitForResponse((r) => r.url().endsWith(`/pages/${wiki.id}/attachments`) && r.request().method() === "GET");
    release();
    await fulfilled;
    await arrived;
    await page.evaluate(() => new Promise((done) => setTimeout(done, 0)));
    await expect(row(page, "budget.csv")).toHaveAttribute("data-version", "1");
  });

  test("a member who may only read the page sees the versions but restores none", async ({ page, api, pageAs }, testInfo) => {
    test.slow();
    const { key, page: wiki } = await aPage(api, testInfo);
    const path = `/s/${key}/p/${wiki.id}/budget`;
    await page.goto(path);
    const input = panel(page).locator("[data-attachment-input]");
    await input.setInputFiles(csv("one\n"));
    await expect(row(page, "budget.csv")).toHaveAttribute("data-version", "1");
    await input.setInputFiles(csv("two\n"));
    await expect(row(page, "budget.csv")).toHaveAttribute("data-version", "2");
    const me = (await api.GET("/auth/me")).data?.user.id;
    const restricted = await api.PUT("/pages/{pageID}/restrictions", {
      params: { path: { pageID: wiki.id } },
      body: { view: [], edit: [{ type: "user", id: String(me) }] },
    });
    expect(restricted.response.status).toBe(200);

    const bob = await pageAs("bob");
    // Bob's read may come from a replica that has the uploads but not the restriction yet.
    await openWithout(bob, path, panel(bob).locator('[data-action="attach-files"]'), row(bob, "budget.csv").getByText("1 earlier version"));
    await row(bob, "budget.csv").getByText("1 earlier version").click();
    await expect(row(bob, "budget.csv").locator('[data-attachment-version="1"]')).toBeVisible();
    await expect(panel(bob).locator('[data-action="restore-attachment"]')).toHaveCount(0);
    await expect(panel(bob).locator('[data-action="delete-attachment-version"]')).toHaveCount(0);
  });
});
