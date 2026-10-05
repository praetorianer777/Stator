import type { Locator, Page, TestInfo } from "@playwright/test";
import type { StatorApi } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { caretTo } from "../fixtures/editor";
import { orgTest as test } from "../fixtures/org";
import { uniqueName } from "../fixtures/seed";
import { expectAccessible, startInScheme, type ColourScheme } from "../fixtures/shell";
import { createPage, createSpace, deleteSpace, publishFromEditor, uniqueKey, type Space } from "../fixtures/spaces";

const editorBox = (page: Page) => page.locator("#page-body");
const shown = (page: Page) => page.locator("[data-doc]");

const paragraph = (value: string) => ({ type: "paragraph", content: [{ type: "text", text: value }] });

// The web container serves the application's own page, the one page inside
// the stack's network the api's guard lets it read; a video's player needs
// no read at all.
const HANDBOOK = "http://web/handbook";
const VIDEO = "https://youtu.be/dQw4w9WgXcQ";
const PLAYER = "https://www.youtube-nocookie.com/embed/dQw4w9WgXcQ";
// The api's unfurl.FetchTimeout. An unreachable site's card is drawn plain
// only once the api gives up on it, which a host whose resolver is slow to
// deny a name makes the whole timeout; the margin is for the answer's way back.
const UNFURL_GIVES_UP_MS = 5_000;
const UNFURL_MARGIN_MS = 5_000;

/** Opens a page's editor until it holds the words given, which a replica may lag behind on. */
async function openEditor(page: Page, path: string, words: string): Promise<void> {
  await expect(async () => {
    await page.goto(`${path}/edit`);
    await expect(editorBox(page)).toContainText(words, { timeout: 2_000 });
  }).toPass();
}

/** Pastes text into the focused editor the way a browser does, as plain text alone. */
async function paste(box: Locator, value: string) {
  await box.evaluate((el, data) => {
    const transfer = new DataTransfer();
    transfer.setData("text/plain", data);
    el.dispatchEvent(new ClipboardEvent("paste", { clipboardData: transfer, bubbles: true, cancelable: true }));
  }, value);
}

test.describe("link previews", { tag: ["@auth"] }, () => {
  const made: string[] = [];
  test.afterEach(async ({ api }) => {
    for (const key of made.splice(0)) await deleteSpace(api, key);
  });

  async function freshSpace(api: StatorApi, testInfo: TestInfo, label: string): Promise<Space> {
    const key = uniqueKey(testInfo);
    made.push(key);
    return createSpace(api, key, uniqueName(testInfo, label));
  }

  test("a pasted address becomes a card, a video's becomes its player, and a card can go inline", async ({ page, api }, testInfo) => {
    const space = await freshSpace(api, testInfo, "Links");
    // The empty paragraph at the end takes the card: an empty line is where
    // an address pasted alone is meant to be shown.
    const notes = await createPage(api, space.homePageId, "Reading", { type: "doc", content: [paragraph("Worth a look."), { type: "paragraph" }] });
    const path = `/s/${space.key}/p/${notes.id}/reading`;

    await openEditor(page, path, "Worth a look.");
    await caretTo(editorBox(page), "end");
    await paste(editorBox(page), HANDBOOK);
    const card = editorBox(page).locator("[data-link-card-edit]");
    await expect(card.locator("[data-link-card-title]")).toHaveText("Stator");
    await expect(card).toContainText("Your team's pages, in Stator.");
    const views = card.getByRole("group", { name: "Show the link as" });
    await expect(views.getByRole("button", { name: "Card" })).toHaveAttribute("aria-pressed", "true");
    await expect(views.getByRole("button", { name: "Embed" })).toBeDisabled();
    await views.getByRole("button", { name: "Inline" }).click();
    await expect(editorBox(page).getByRole("link", { name: "Stator" })).toHaveAttribute("href", HANDBOOK);
    await expect(card).toHaveCount(0);

    await caretTo(editorBox(page), "end");
    await page.keyboard.press("Enter");
    await paste(editorBox(page), VIDEO);
    await expect(editorBox(page).locator(`iframe[src="${PLAYER}"]`)).toHaveCount(1);
    await expect(editorBox(page).locator('[data-link-card-view="embed"]')).toHaveAttribute("aria-pressed", "true");

    await expect(page.locator("[data-draft-status]")).toHaveAttribute("data-draft-status", "saved");
    await publishFromEditor(page);

    await expect(shown(page).getByRole("link", { name: "Stator" })).toHaveAttribute("href", HANDBOOK);
    const player = shown(page).locator(`[data-link-card="embed"] iframe[src="${PLAYER}"]`);
    await expect(player).toHaveAttribute("sandbox", /allow-scripts/);
    await expect(shown(page).locator('[data-link-card="embed"] a.doc-link-card-body')).toHaveAttribute("href", VIDEO);
  });

  test("a link preview is asked for from the slash menu", async ({ page, api }, testInfo) => {
    const space = await freshSpace(api, testInfo, "Slash");
    const notes = await createPage(api, space.homePageId, "Slash", { type: "doc", content: [paragraph("Below."), { type: "paragraph" }] });
    await openEditor(page, `/s/${space.key}/p/${notes.id}/slash`, "Below.");
    await caretTo(editorBox(page), "end");
    await page.keyboard.type("/link");
    await expect(page.getByRole("listbox", { name: "Insert a block" })).toBeVisible();
    await page.keyboard.press("Enter");
    const dialog = page.getByRole("dialog", { name: "Link preview" });
    await dialog.getByLabel("Address").fill("handbook");
    await dialog.getByRole("button", { name: "Insert" }).click();
    await expect(dialog.getByText(/starting with https:\/\/ or http:\/\//)).toBeVisible();
    await dialog.getByLabel("Address").fill(HANDBOOK);
    await dialog.getByRole("button", { name: "Insert" }).click();
    await expect(editorBox(page).locator("[data-link-card-title]")).toHaveText("Stator");
  });

  for (const scheme of ["light", "dark"] as ColourScheme[]) {
    test(`link cards pass axe in ${scheme}`, async ({ page, api }, testInfo) => {
      const space = await freshSpace(api, testInfo, "Axe");
      const notes = await createPage(api, space.homePageId, "Cards", {
        type: "doc",
        content: [
          { type: "linkCard", attrs: { url: HANDBOOK, view: "card" } },
          { type: "linkCard", attrs: { url: VIDEO, view: "embed" } },
          { type: "linkCard", attrs: { url: "https://unreachable.invalid/post", view: "card" } },
        ],
      });
      await startInScheme(page, scheme);
      await expect(async () => {
        await page.goto(`/s/${space.key}/p/${notes.id}/cards`);
        await expect(shown(page).locator("[data-link-card-title]").first()).toHaveText("Stator", { timeout: 2_000 });
      }).toPass();
      await expect(shown(page).locator("iframe")).toHaveCount(1);
      await expect(shown(page).locator('[data-link-card="card"][data-state="plain"]')).toContainText("unreachable.invalid/post", {
        timeout: UNFURL_GIVES_UP_MS + UNFURL_MARGIN_MS,
      });
      // The player inside the embed is the video site's own markup (#275), so
      // axe leaves the frame out and its title, which is ours, is checked here.
      await expect(shown(page).locator("iframe")).toHaveAttribute("title", /\S/);
      await expectAccessible(page, { thirdPartyFrames: ["main [data-doc] iframe"] });
    });
  }
});
