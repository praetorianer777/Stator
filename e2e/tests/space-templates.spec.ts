import type { Page } from "@playwright/test";
import { must } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { orgTest as test } from "../fixtures/org";
import { uniqueName } from "../fixtures/seed";
import { expectAccessible } from "../fixtures/shell";
import { childTitles, deleteSpace, uniqueKey } from "../fixtures/spaces";

const picker = (page: Page) => page.locator("[data-space-template-picker]");
const option = (page: Page, name: string) => picker(page).getByRole("radio", { name: new RegExp(`^${name}`) });
const preview = (page: Page) => page.locator("[data-space-template-preview]");

test.describe("space templates", { tag: ["@auth"] }, () => {
  const made: string[] = [];
  test.afterEach(async ({ api }) => {
    for (const key of made.splice(0)) await deleteSpace(api, key);
  });

  test("an administrator makes a knowledge base with its pages, labels and permissions", async ({ page, api, pageAs }, testInfo) => {
    const key = uniqueKey(testInfo);
    const name = uniqueName(testInfo, "Answers");
    made.push(key);

    await page.goto("/spaces/new");
    await expect(option(page, "Blank space")).toHaveAttribute("aria-checked", "true");
    await option(page, "Knowledge base").click();
    await expect(option(page, "Knowledge base")).toHaveAttribute("aria-checked", "true");
    await expect(preview(page)).toHaveAttribute("data-space-template-preview", "knowledge-base");
    await expect(preview(page).locator('[data-template-page="Frequently asked questions"]')).toContainText("faq");
    await expect(preview(page).locator("[data-space-template-everyone]")).toHaveAttribute("data-space-template-everyone", "view addPages addComments");
    await expectAccessible(page);

    await page.getByLabel("Name", { exact: true }).fill(name);
    await page.getByLabel("Key", { exact: true }).fill(key);
    await page.locator('[data-action="create-space"]').click();
    await expect(page).toHaveURL(new RegExp(`/s/${key}$`));
    const doc = page.locator("[data-doc]");
    await expect(doc).toContainText("This knowledge base collects answers");
    await expect(doc.getByRole("heading", { name: "How-to articles" })).toBeVisible();
    // The list by label on the home page finds the article the template labelled.
    await expect(doc.getByRole("link", { name: "How to write a how-to article" }).first()).toBeVisible();

    const space = must(await api.GET("/spaces/{spaceKey}", { params: { path: { spaceKey: key } } })).space;
    expect(await childTitles(api, key, space.homePageId)).toEqual(["How-to articles", "Troubleshooting", "Frequently asked questions", "Contributing"]);
    const { pages } = must(await api.GET("/labels/{labelName}/pages", { params: { path: { labelName: "faq" }, query: { space: key } } }));
    expect(pages.map((each) => each.title)).toEqual(["Frequently asked questions"]);
    const { grants } = must(await api.GET("/spaces/{spaceKey}/permissions", { params: { path: { spaceKey: key } } }));
    expect(grants.find((g) => g.subject.type === "everyone")?.permissions).toEqual(["view", "addPages", "addComments"]);

    // Everybody in the organization reads it, which a replica may not have yet.
    const bob = await pageAs("bob");
    await expect(async () => {
      await bob.goto(`/s/${key}`);
      await expect(bob.locator("[data-doc]")).toContainText("This knowledge base collects answers", { timeout: 1_000 });
    }).toPass();
  });
});
