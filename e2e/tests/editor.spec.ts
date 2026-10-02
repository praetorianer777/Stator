import type { Locator, Page } from "@playwright/test";
import { expect, test } from "../fixtures/auth";
import { expectAccessible, startInScheme } from "../fixtures/shell";

// The editor on its development page, which pages will replace. Every state
// the checklist reaches is also checked with axe, contrast included.
const EDITOR_PATH = "/dev/editor";
const SLASH_ITEM_COUNT = 29;
const TABLE_SIZE = 3;
// Enough paragraphs that the stored document outgrows its box.
const STORED_LINES = 12;
// A swipe is a finger dragged this far, in this many moves, starting this far
// inside the list's lower edge.
const SWIPE_PX = 400;
const SWIPE_STEPS = 10;
const SWIPE_INSET_PX = 12;
// The slash menu's options wrap on a narrow screen, so reaching its end takes
// a few swipes, as it would take a thumb.
const MAX_SWIPES = 4;
// The development page offers eight people, which never outgrow the mention
// list, so the swipe test gives the list less room to have something to scroll.
const SHORT_LIST_CSS = "[data-mention-list] { max-height: 6rem; }";
// One class for each colour the stylesheet gives highlighted code, all of
// which the development page's sample reaches.
const HIGHLIGHT_CLASSES = [
  "hljs-comment",
  "hljs-keyword",
  "hljs-string",
  "hljs-number",
  "hljs-title",
  "hljs-built_in",
  "hljs-variable",
  "hljs-meta",
  "hljs-addition",
  "hljs-deletion",
];

const box = (page: Page) => page.locator("#dev-editor");
const preview = (page: Page) => page.locator("[data-dev-preview]");
const tools = (page: Page, kind: string) => page.locator(`[data-editor-tools="${kind}"]`);
const action = (page: Page, name: string) => page.locator(`[data-editor-action="${name}"]`);

type DocNode = { type: string; attrs?: Record<string, unknown>; content?: DocNode[]; text?: string };

async function storedDoc(page: Page): Promise<DocNode | null> {
  return JSON.parse((await page.locator("[data-dev-json]").textContent()) || "null") as DocNode | null;
}

function nodes(doc: DocNode | null, type: string): DocNode[] {
  if (!doc) return [];
  const found = doc.type === type ? [doc] : [];
  return found.concat(...(doc.content ?? []).map((child) => nodes(child, type)));
}

/** Opens the editor with nothing in it but an empty paragraph, the caret in it. */
async function emptyEditor(page: Page) {
  await page.goto(EDITOR_PATH);
  await box(page).click();
  await page.keyboard.press("ControlOrMeta+a");
  await page.keyboard.press("Backspace");
  await page.keyboard.type("/text");
  await page.keyboard.press("Enter");
  await expect(box(page)).toHaveText("");
  await expect(box(page).locator("> *")).toHaveCount(1);
  expect(
    await box(page)
      .locator("> *")
      .evaluate((el) => el.tagName),
  ).toBe("P");
}

/** Whether an option is drawn wholly inside the list that clips it. */
async function shownWithin(list: Locator, option: Locator): Promise<boolean> {
  const [outer, inner] = await Promise.all([list.boundingBox(), option.boundingBox()]);
  return !!outer && !!inner && inner.y >= outer.y && inner.y + inner.height <= outer.y + outer.height;
}

/** Drags one finger up across a list, through the browser's own touch input. */
async function swipeUp(page: Page, list: Locator) {
  const area = await list.boundingBox();
  if (!area) throw new Error("The list to swipe is not on the screen.");
  const x = area.x + area.width / 2;
  const from = area.y + area.height - SWIPE_INSET_PX;
  const cdp = await page.context().newCDPSession(page);
  await cdp.send("Input.dispatchTouchEvent", { type: "touchStart", touchPoints: [{ x, y: from }] });
  for (let step = 1; step <= SWIPE_STEPS; step++) {
    await cdp.send("Input.dispatchTouchEvent", { type: "touchMove", touchPoints: [{ x, y: from - (SWIPE_PX * step) / SWIPE_STEPS }] });
  }
  await cdp.send("Input.dispatchTouchEvent", { type: "touchEnd", touchPoints: [] });
  await cdp.detach();
}

async function insert(page: Page, query: string) {
  await page.keyboard.type(`/${query}`);
  await expect(page.getByRole("listbox", { name: "Insert a block" })).toBeVisible();
  await page.keyboard.press("Enter");
}

test.describe("the editor", { tag: "@desktop" }, () => {
  test("the slash menu lists every block, filters, inserts and closes", async ({ page }) => {
    await emptyEditor(page);
    await page.keyboard.type("/");
    const menu = page.getByRole("listbox", { name: "Insert a block" });
    const options = menu.getByRole("option");
    await expect(options).toHaveCount(SLASH_ITEM_COUNT);
    for (const option of await options.all()) {
      await expect(option.locator("span.text-xs")).not.toBeEmpty();
    }
    await expect(options.nth(0)).toHaveAttribute("aria-selected", "true");
    await page.keyboard.press("ArrowDown");
    await expect(options.nth(1)).toHaveAttribute("aria-selected", "true");
    await expect(options.nth(0)).toHaveAttribute("aria-selected", "false");
    await expectAccessible(page);

    // Past the top the active option wraps to the last one, which the list
    // brings into view; the wheel still scrolls the list back.
    await page.keyboard.press("ArrowUp");
    await page.keyboard.press("ArrowUp");
    await expect(options.last()).toHaveAttribute("aria-selected", "true");
    await expect.poll(() => shownWithin(menu, options.last())).toBe(true);
    await expect.poll(() => shownWithin(menu, options.first())).toBe(false);
    await expect(box(page)).toBeFocused();
    await menu.hover();
    await page.mouse.wheel(0, -2000);
    await expect.poll(() => shownWithin(menu, options.first())).toBe(true);
    await expectAccessible(page);

    await page.keyboard.type("quo");
    await expect(options).toHaveCount(1);
    await expect(options.first()).toContainText("Quote");
    await page.keyboard.press("Enter");
    await expect(box(page).locator("blockquote")).toHaveCount(1);
    await expect(box(page)).not.toContainText("/quo");

    await page.keyboard.type("/zzzz");
    await expect(page.locator("[data-slash-empty]")).toHaveText("No block matches. Keep typing, or press Escape.");
    await expectAccessible(page);
    await page.keyboard.press("Escape");
    await expect(page.locator("[data-slash-menu]")).toHaveCount(0);
  });

  test("a table grows, shrinks, merges, splits, takes a background and goes", async ({ page }) => {
    await emptyEditor(page);
    await insert(page, "table");
    const table = box(page).locator("table");
    await expect(table.locator("tr")).toHaveCount(TABLE_SIZE);
    await expect(table.locator("tr").first().locator("th")).toHaveCount(TABLE_SIZE);
    await expect(tools(page, "table")).toBeVisible();
    // A header naming nothing is a real finding; a real table has words there.
    await page.keyboard.type("Name");
    await page.keyboard.press("Tab");
    await page.keyboard.type("Role");
    await page.keyboard.press("Tab");
    await page.keyboard.type("Team");
    await expectAccessible(page);

    await action(page, "row-below").click();
    await expect(table.locator("tr")).toHaveCount(TABLE_SIZE + 1);
    await action(page, "column-after").click();
    await expect(table.locator("tr").first().locator("th, td")).toHaveCount(TABLE_SIZE + 1);
    // Into the new, empty column, so the one deleted is the one just added.
    await page.keyboard.press("Tab");
    await action(page, "delete-column").click();
    await expect(table.locator("tr").first().locator("th, td")).toHaveCount(TABLE_SIZE);

    const firstBodyCell = table.locator("tr").nth(1).locator("th, td").first();
    await action(page, "header-column").click();
    await expect.poll(() => firstBodyCell.evaluate((el) => el.tagName)).toBe("TH");
    await action(page, "header-column").click();
    await expect.poll(() => firstBodyCell.evaluate((el) => el.tagName)).toBe("TD");

    const row = table.locator("tr").nth(1).locator("td");
    const from = (await row.nth(0).boundingBox())!;
    const to = (await row.nth(1).boundingBox())!;
    await page.mouse.move(from.x + from.width / 2, from.y + from.height / 2);
    await page.mouse.down();
    await page.mouse.move(to.x + to.width / 2, to.y + to.height / 2, { steps: 5 });
    await page.mouse.up();
    await action(page, "merge-cells").click();
    await expect(table.locator('td[colspan="2"]')).toHaveCount(1);
    await action(page, "split-cell").click();
    await expect(table.locator('td[colspan="2"]')).toHaveCount(0);

    await table.locator("tr").nth(2).locator("td").first().click();
    await action(page, "cell-background").click();
    await expect(page.getByRole("menu", { name: "Cell background" })).toBeVisible();
    await expectAccessible(page);
    await page.locator('[role="menu"] [data-background-option="warning"]').click();
    await expect(table.locator('td[data-background="warning"]')).toHaveCount(1);
    await expect(preview(page).locator('td[data-background="warning"]')).toHaveCount(1);
    await expectAccessible(page);

    await table.locator("tr").nth(2).locator("td").first().click();
    await action(page, "delete-table").click();
    await expect(box(page).locator("table")).toHaveCount(0);
  });

  test("a code block takes a language and is highlighted in the editor and the preview", async ({ page }) => {
    await emptyEditor(page);
    await insert(page, "code");
    await page.keyboard.type("def main(): return 1");
    await action(page, "language").selectOption("python");
    await expect.poll(async () => nodes(await storedDoc(page), "codeBlock")[0]?.attrs?.language).toBe("python");

    for (const where of [box(page), preview(page)]) {
      const keyword = where.locator(".hljs-keyword").first();
      await expect(keyword).toHaveText("def");
      const plain = await where.locator("pre code").evaluate((el) => getComputedStyle(el).color);
      expect(await keyword.evaluate((el) => getComputedStyle(el).color)).not.toBe(plain);
    }
    await expect(preview(page).locator("pre .doc-code-language")).toHaveText("Python");
    await expectAccessible(page);
  });

  for (const scheme of ["light", "dark"] as const) {
    test(`highlighted code in several languages passes axe contrast in ${scheme}`, async ({ page }) => {
      await startInScheme(page, scheme);
      await page.goto(EDITOR_PATH);
      await expect(page.locator("html")).toHaveAttribute("data-theme", scheme);
      for (const where of [box(page), preview(page)]) {
        for (const token of HIGHLIGHT_CLASSES) await expect(where.locator(`pre .${token}`).first(), token).toBeVisible();
      }
      await expectAccessible(page);
    });
  }

  test("a panel changes kind, follows the theme's colours and can be removed", async ({ page }) => {
    await emptyEditor(page);
    await insert(page, "warning");
    await page.keyboard.type("Careful");
    await expect(box(page).locator('[data-panel="warning"][role="note"]')).toBeVisible();
    await expectAccessible(page);

    await action(page, "panel-kind").selectOption("error");
    await expect(box(page).locator('[data-panel="error"]')).toBeVisible();
    await expect(preview(page).locator('[data-panel="error"]')).toBeVisible();

    const fill = () =>
      preview(page)
        .locator('[data-panel="error"]')
        .evaluate((el) => getComputedStyle(el).backgroundColor);
    const token = () =>
      page.evaluate(() => {
        const probe = document.createElement("div");
        probe.style.background = "var(--color-danger-subtle)";
        document.body.append(probe);
        const colour = getComputedStyle(probe).backgroundColor;
        probe.remove();
        return colour;
      });
    expect(await fill()).toBe(await token());

    // A custom theme is a stylesheet of variables over the stock ones, as this is.
    await page.addStyleTag({ content: ":root { --color-danger-subtle: #ffd0e0; }" });
    expect(await fill()).toBe("rgb(255, 208, 224)");

    await action(page, "remove-panel").click();
    await expect(box(page).locator("[data-panel]")).toHaveCount(0);
    await expect(box(page)).toContainText("Careful");
  });

  test("a panel follows the dark palette too", async ({ page }) => {
    await startInScheme(page, "dark");
    await emptyEditor(page);
    await insert(page, "error");
    await page.keyboard.type("Stop");
    const panel = preview(page).locator('[data-panel="error"]');
    await expect(panel).toBeVisible();
    const expected = await page.evaluate(() => {
      const probe = document.createElement("div");
      probe.style.background = "var(--color-danger-subtle)";
      document.body.append(probe);
      const colour = getComputedStyle(probe).backgroundColor;
      probe.remove();
      return colour;
    });
    expect(await panel.evaluate((el) => getComputedStyle(el).backgroundColor)).toBe(expected);
    await expectAccessible(page);
  });

  test("headings get anchors, a twin gets its own, and a copied link leads back", async ({ page, context }) => {
    await context.grantPermissions(["clipboard-read", "clipboard-write"]);
    await emptyEditor(page);
    await insert(page, "h1");
    await page.keyboard.type("Release plan");
    await expect.poll(async () => nodes(await storedDoc(page), "heading").map((h) => h.attrs?.id)).toEqual(["release-plan"]);
    await expect(preview(page).locator("h2#release-plan")).toHaveText("Release plan");

    await page.keyboard.press("Enter");
    await insert(page, "h1");
    await page.keyboard.type("Release plan");
    await expect.poll(async () => nodes(await storedDoc(page), "heading").map((h) => h.attrs?.id)).toEqual(["release-plan", "release-plan-2"]);
    await expectAccessible(page);

    await box(page).locator("h2").first().click();
    await action(page, "copy-heading-link").click();
    await expect(page.locator("[data-copy-status]").first()).toHaveText("Link copied");
    const link = await page.evaluate(() => navigator.clipboard.readText());
    expect(link).toBe(`${new URL(page.url()).origin}${EDITOR_PATH}#release-plan`);

    // The page keeps nothing yet, so the link is followed on the page that holds the heading.
    await page.evaluate(() => window.scrollTo(0, 0));
    await page.evaluate((hash) => {
      window.location.hash = hash;
    }, new URL(link).hash);
    await expect(preview(page).locator("h2#release-plan")).toBeInViewport();
  });

  test("the stored document scrolls from the keyboard", async ({ page }) => {
    await emptyEditor(page);
    for (let line = 0; line < STORED_LINES; line++) await page.keyboard.type(`Line ${line}\n`);
    const stored = page.getByRole("region", { name: "Stored document as JSON" });
    await expect.poll(() => stored.evaluate((el) => el.scrollHeight > el.clientHeight)).toBe(true);
    await stored.focus();
    await expect(stored).toBeFocused();
    await page.keyboard.press("PageDown");
    await expect.poll(() => stored.evaluate((el) => el.scrollTop)).toBeGreaterThan(0);
    await expectAccessible(page);
  });
});

test.describe("the editor on a touch screen", { tag: "@mobile" }, () => {
  test("a swipe scrolls the slash menu and a tap picks from it", async ({ page }) => {
    await emptyEditor(page);
    await page.keyboard.type("/");
    const menu = page.getByRole("listbox", { name: "Insert a block" });
    const options = menu.getByRole("option");
    await expect.poll(() => menu.evaluate((el) => el.scrollHeight > el.clientHeight)).toBe(true);
    await expect.poll(() => shownWithin(menu, options.last())).toBe(false);
    await expectAccessible(page);

    const pageTop = await page.evaluate(() => window.scrollY);
    await swipeUp(page, menu);
    await expect.poll(() => menu.evaluate((el) => el.scrollTop)).toBeGreaterThan(0);
    for (let swipe = 1; swipe < MAX_SWIPES && !(await shownWithin(menu, options.last())); swipe++) await swipeUp(page, menu);
    await expect.poll(() => shownWithin(menu, options.last())).toBe(true);
    expect(await page.evaluate(() => window.scrollY)).toBe(pageTop);
    await expect(box(page)).toBeFocused();
    await expectAccessible(page);

    // The last block, scrolled to, opens the emoji list, whose first emoji a tap puts in.
    await options.last().tap();
    await expect(page.locator("[data-slash-menu]")).toHaveCount(0);
    const emoji = page.getByRole("listbox", { name: "Emoji" }).getByRole("option");
    await expect(emoji.first()).toContainText(":+1:");
    await emoji.first().tap();
    await expect(box(page)).toHaveText("👍");
    await expect(box(page)).toBeFocused();
  });

  test("a swipe scrolls the mention list and a tap picks from it", async ({ page }) => {
    await emptyEditor(page);
    await page.addStyleTag({ content: SHORT_LIST_CSS });
    await page.keyboard.type("@a");
    const list = page.getByRole("listbox", { name: "People to mention" });
    const options = list.getByRole("option");
    await expect.poll(() => list.evaluate((el) => el.scrollHeight > el.clientHeight)).toBe(true);
    await expect.poll(() => shownWithin(list, options.last())).toBe(false);
    await expectAccessible(page);

    const pageTop = await page.evaluate(() => window.scrollY);
    await swipeUp(page, list);
    await expect.poll(() => shownWithin(list, options.last())).toBe(true);
    expect(await page.evaluate(() => window.scrollY)).toBe(pageTop);
    await expect(box(page)).toBeFocused();
    await expectAccessible(page);

    const name = await options.last().getAttribute("data-mention-option");
    await options.last().tap();
    await expect(list).toHaveCount(0);
    await expect(box(page).locator("[data-mention]")).toHaveText(`@${name}`);
    await expect(box(page)).toBeFocused();
  });
});
