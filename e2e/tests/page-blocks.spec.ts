import type { Page, TestInfo } from "@playwright/test";
import { must, type StatorApi } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { caretTo, focusEditor } from "../fixtures/editor";
import { orgTest as test } from "../fixtures/org";
import { uniqueName } from "../fixtures/seed";
import { expectAccessible, startInScheme, type ColourScheme } from "../fixtures/shell";
import { createPage, createSpace, deleteSpace, publishFromEditor, uniqueKey, type Space } from "../fixtures/spaces";

const heading = (page: Page) => page.locator("main").getByRole("heading", { level: 1 });
const editorBox = (page: Page) => page.locator("#page-body");
const toc = (scope: Page | ReturnType<Page["locator"]>) => scope.getByRole("navigation", { name: "Table of contents" });
const children = (scope: Page | ReturnType<Page["locator"]>) => scope.getByRole("navigation", { name: "Child pages" });

// Enough words between the headings that the second one starts below the fold.
const FILLER_PARAGRAPHS = 40;

const text = (value: string) => [{ type: "text", text: value }];
const paragraph = (value?: string) => (value ? { type: "paragraph", content: text(value) } : { type: "paragraph" });
const h = (level: number, value: string, id: string) => ({ type: "heading", attrs: { level, id }, content: text(value) });
const filler = () => Array.from({ length: FILLER_PARAGRAPHS }, (_, i) => paragraph(`Line ${i + 1} of the notes that sit between the two sections.`));

/** Opens a page until the child pages block lists a title, which a replica may lag behind on. */
async function openListing(page: Page, path: string, title: string): Promise<void> {
  await expect(async () => {
    await page.goto(path);
    await expect(children(page).getByRole("link", { name: title })).toBeVisible({ timeout: 1_000 });
  }).toPass();
}

async function userId(api: StatorApi): Promise<string> {
  return must(await api.GET("/auth/me")).user.id;
}

async function insert(page: Page, query: string): Promise<void> {
  await page.keyboard.type(`/${query}`);
  await expect(page.getByRole("listbox", { name: "Insert a block" })).toBeVisible();
  await page.keyboard.press("Enter");
}

test.describe("table of contents and child pages", { tag: ["@auth", "@desktop"] }, () => {
  const made: string[] = [];
  test.afterEach(async ({ api }) => {
    for (const key of made.splice(0)) await deleteSpace(api, key);
  });

  async function freshSpace(api: StatorApi, testInfo: TestInfo, label: string): Promise<Space> {
    const key = uniqueKey(testInfo);
    made.push(key);
    return createSpace(api, key, uniqueName(testInfo, label));
  }

  test("both blocks go in from the slash menu, and their links lead to the heading and the page", async ({ page, api }, testInfo) => {
    const space = await freshSpace(api, testInfo, "Blocks");
    const guide = await createPage(api, space.homePageId, "Guide", {
      type: "doc",
      content: [
        paragraph(),
        h(1, "Install", "install"),
        ...filler(),
        h(1, "Troubleshooting", "troubleshooting"),
        h(2, "Logs", "logs"),
        paragraph("The end."),
        paragraph(),
      ],
    });
    const setup = await createPage(api, guide.id, "Setup");
    await createPage(api, setup.id, "Linux");

    await expect(async () => {
      await page.goto(`/s/${space.key}/p/${guide.id}/guide/edit`);
      await expect(editorBox(page)).toContainText("Troubleshooting", { timeout: 2_000 });
    }).toPass();
    await caretTo(editorBox(page), "start");
    await insert(page, "contents");
    const contents = toc(editorBox(page));
    await expect(contents.getByRole("link")).toHaveText(["Install", "Troubleshooting", "Logs"]);
    await contents.getByLabel("Headings to list").selectOption({ label: "Heading 1 only" });
    await expect(contents.getByRole("link")).toHaveText(["Install", "Troubleshooting"]);

    // The empty paragraph at the end takes the block: an Enter after the caret
    // key would split wherever ProseMirror last saw the caret, as it reads the
    // browser's move only when the selection change event arrives.
    await focusEditor(editorBox(page).locator("p", { hasText: "The end." }));
    await page.keyboard.press("ControlOrMeta+End");
    await insert(page, "child");
    const list = children(editorBox(page));
    await expect(list.getByRole("link", { name: "Setup" })).toBeVisible();
    await expect(list.getByRole("link", { name: "Linux" })).toHaveCount(0);
    await list.getByLabel("Pages to list").selectOption({ label: "All pages below" });
    await expect(list.getByRole("link", { name: "Linux" })).toBeVisible();
    await expect(page.locator("[data-draft-status]")).toHaveAttribute("data-draft-status", "saved");
    await publishFromEditor(page);
    // Opened afresh, so the page starts at its top rather than where the editor was scrolled to.
    await page.goto(`/s/${space.key}/p/${guide.id}/guide`);

    const shown = toc(page.locator("[data-doc]"));
    await expect(shown.getByRole("link")).toHaveText(["Install", "Troubleshooting"]);
    const target = page.locator("[data-doc] h2#troubleshooting");
    await expect(target).not.toBeInViewport();
    await shown.getByRole("link", { name: "Troubleshooting" }).click();
    await expect(target).toBeInViewport();
    await expect(page).toHaveURL(new RegExp(`/p/${guide.id}/guide#troubleshooting$`));

    await children(page.locator("[data-doc]")).getByRole("link", { name: "Linux" }).click();
    await expect(heading(page)).toHaveText("Linux");
  });

  test("a child list leaves out a page restricted from its reader", async ({ page, api, apiAs, pageAs }, testInfo) => {
    const space = await freshSpace(api, testInfo, "Restricted");
    const guide = await createPage(api, space.homePageId, "Team", {
      type: "doc",
      content: [{ type: "childPages", attrs: { scope: "subtree", depth: null, sort: "title" } }],
    });
    await createPage(api, guide.id, "Open notes");
    const secret = await createPage(api, guide.id, "Salaries");
    await createPage(api, secret.id, "Salary bands");
    must(
      await api.PUT("/pages/{pageID}/restrictions", {
        params: { path: { pageID: secret.id } },
        body: { view: [{ type: "user", id: await userId(api) }], edit: [] },
      }),
    );

    await openListing(page, `/s/${space.key}/p/${guide.id}/team`, "Salary bands");
    await expect(children(page).getByRole("link")).toHaveText(["Open notes", "Salaries", "Salary bands"]);

    const bobApi = await apiAs("bob");
    await expect.poll(async () => (await bobApi.GET("/pages/{pageID}", { params: { path: { pageID: secret.id } } })).response.status).toBe(404);
    const bob = await pageAs("bob");
    await openListing(bob, `/s/${space.key}/p/${guide.id}/team`, "Open notes");
    await expect(children(bob).getByRole("link")).toHaveText(["Open notes"]);
    await expect(bob.getByText("Salar")).toHaveCount(0);
  });

  for (const scheme of ["light", "dark"] as ColourScheme[]) {
    test(`both blocks pass axe in ${scheme}, read and in the editor`, async ({ page, api }, testInfo) => {
      const space = await freshSpace(api, testInfo, "Axe");
      const guide = await createPage(api, space.homePageId, "Field guide", {
        type: "doc",
        content: [
          { type: "tableOfContents", attrs: { maxLevel: 3 } },
          h(1, "Birds", "birds"),
          h(2, "Puffins", "puffins"),
          paragraph("Small and bright."),
          { type: "childPages", attrs: { scope: "subtree", depth: 2, sort: "updated" } },
        ],
      });
      const first = await createPage(api, guide.id, "Seabirds");
      await createPage(api, first.id, "Gannets");
      await startInScheme(page, scheme);

      await openListing(page, `/s/${space.key}/p/${guide.id}/field-guide`, "Gannets");
      await expect(page.locator("html")).toHaveAttribute("data-theme", scheme);
      await expect(toc(page).getByRole("link")).toHaveText(["Birds", "Puffins"]);
      await expectAccessible(page);

      await page.locator('[data-action="edit-page"]').click();
      await expect(children(editorBox(page)).getByRole("link", { name: "Gannets" })).toBeVisible();
      await expect(toc(editorBox(page)).getByLabel("Headings to list")).toBeVisible();
      await expectAccessible(page);
    });
  }
});
