import type { Page, TestInfo } from "@playwright/test";
import { must, type StatorApi } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { caretTo } from "../fixtures/editor";
import { png, upload } from "../fixtures/files";
import { orgTest as test } from "../fixtures/org";
import { ONE_LOOK, openShowing, openUntil } from "../fixtures/replica";
import { uniqueName } from "../fixtures/seed";
import { expectAccessible, scrollsSideways, startInScheme, type ColourScheme } from "../fixtures/shell";
import { createPage, createSpace, deleteSpace, publishFromEditor, uniqueKey } from "../fixtures/spaces";
import { WEB_URL } from "../fixtures/stack";

const ANONYMOUS = { cookies: [], origins: [] };
const PICTURE_WIDTH = 640;
const PICTURE_HEIGHT = 400;

const editorBox = (page: Page) => page.locator("#page-body");
const shown = (page: Page) => page.locator("main [data-doc]");
const lightbox = (page: Page) => page.getByRole("dialog");
const position = (page: Page) => lightbox(page).locator("[data-lightbox-position]");
const paragraph = (value: string) => ({ type: "paragraph", content: [{ type: "text", text: value }] });
const picture = (attachmentId: string, caption: string | null) => ({ type: "galleryImage", attrs: { attachmentId, caption } });

/** A space with a page of two screenshots and a text file, the page's words only. */
async function screensPage(api: StatorApi, testInfo: TestInfo, made: string[]) {
  const key = uniqueKey(testInfo);
  made.push(key);
  const space = await createSpace(api, key, uniqueName(testInfo, "Screens"));
  const target = await createPage(api, space.homePageId, "Release screens", { type: "doc", content: [paragraph("What changed on screen.")] });
  const login = await upload(api, target.id, "login.png", "image/png", png(PICTURE_WIDTH, PICTURE_HEIGHT, [30, 90, 160]));
  const settings = await upload(api, target.id, "settings.png", "image/png", png(PICTURE_HEIGHT, PICTURE_WIDTH, [200, 60, 40]));
  await upload(api, target.id, "notes.txt", "text/plain", Buffer.from("Shot on the staging site."));
  return { key, space, target, login, settings, path: `/s/${key}/p/${target.id}/release-screens` };
}

async function writeBody(api: StatorApi, pageId: string, content: unknown[]) {
  must(await api.PATCH("/pages/{pageID}", { params: { path: { pageID: pageId } }, body: { version: 1, body: { type: "doc", content } as never } }));
}

test.describe("image galleries", { tag: ["@auth"] }, () => {
  const made: string[] = [];
  test.afterEach(async ({ api }) => {
    for (const key of made.splice(0)) await deleteSpace(api, key);
  });

  test("an author makes a gallery of the page's screenshots and a new one, and a reader steps through it", async ({ page, api }, testInfo) => {
    test.slow();
    const { path } = await screensPage(api, testInfo, made);

    await openShowing(page, `${path}/edit`, "What changed on screen.");
    await caretTo(editorBox(page), "end");
    await page.keyboard.press("Enter");
    await page.keyboard.type("/gallery");
    await expect(page.getByRole("listbox", { name: "Insert a block" })).toBeVisible();
    await page.keyboard.press("Enter");
    const dialog = page.getByRole("dialog", { name: "Insert a gallery" });
    const login = dialog.getByRole("checkbox", { name: "login.png" });
    await expect(login).toBeVisible();
    await expect(dialog.getByRole("checkbox", { name: "notes.txt" })).toHaveCount(0);
    await login.check();
    await dialog.getByRole("checkbox", { name: "settings.png" }).check();
    await dialog
      .locator("[data-gallery-upload]")
      .setInputFiles({ name: "dashboard.png", mimeType: "image/png", buffer: png(PICTURE_WIDTH, PICTURE_WIDTH, [40, 160, 90]) });
    await expect(dialog.getByLabel("Caption for dashboard.png")).toBeVisible();
    await dialog.getByRole("button", { name: "Move dashboard.png earlier" }).click();
    await dialog.getByLabel("Caption for login.png").fill("The new sign-in");
    await dialog.getByLabel("Caption for dashboard.png").fill("The dashboard");
    await dialog.getByLabel("Pictures in a row").selectOption("2");
    await expect(dialog.locator("[data-gallery-chosen-picture]")).toHaveText([/login\.png/, /dashboard\.png/, /settings\.png/]);
    await expectAccessible(page);
    await dialog.getByRole("button", { name: "Insert gallery" }).click();
    await expect(dialog).toHaveCount(0);

    const inEditor = editorBox(page).locator("[data-gallery-node]");
    await expect(inEditor).toContainText("3 pictures, up to 2 in a row");
    await expect(inEditor.locator("[data-gallery-picture]")).toHaveCount(3);
    await expect(page.locator("[data-draft-status]")).toHaveAttribute("data-draft-status", "saved");
    await publishFromEditor(page);

    const gallery = shown(page).getByRole("list", { name: "Gallery of 3 pictures" });
    await expect(gallery).toHaveAttribute("data-columns", "2");
    await expect(gallery.locator("figcaption")).toHaveText(["The new sign-in", "The dashboard"]);
    await expect(gallery.locator("img").first()).toHaveAttribute("loading", "lazy");
    await expect.poll(() => gallery.locator("img").evaluateAll((imgs: HTMLImageElement[]) => imgs.every((img) => img.naturalWidth > 0))).toBe(true);
    // Two in a row on every screen this suite runs at: as stored on a wide one, and two on a phone.
    const tops = await gallery.locator("li").evaluateAll((items) => items.map((li) => Math.round(li.getBoundingClientRect().top)));
    expect(tops[0]).toBe(tops[1]);
    expect(tops[2]).toBeGreaterThan(tops[0]!);
    expect(await scrollsSideways(page)).toBe(false);

    const opener = gallery.getByRole("button", { name: "View The dashboard larger, 2 of 3" });
    await opener.click();
    await expect(lightbox(page)).toHaveAccessibleName("The dashboard");
    await expect(position(page)).toHaveText("2 of 3");
    await page.keyboard.press("ArrowRight");
    await expect(lightbox(page)).toHaveAccessibleName("Picture");
    await expect(position(page)).toHaveText("3 of 3");
    await lightbox(page).getByRole("button", { name: "Next" }).click();
    await expect(lightbox(page)).toHaveAccessibleName("The new sign-in");
    await page.keyboard.press("ArrowLeft");
    await expect(position(page)).toHaveText("3 of 3");
    await page.keyboard.press("Escape");
    await expect(lightbox(page)).toHaveCount(0);
    await expect(opener).toBeFocused();

    // Taking a picture out again, from the block in the editor.
    await openShowing(page, `${path}/edit`, inEditor.locator("[data-gallery-picture]").first());
    await inEditor.getByRole("button", { name: "Edit gallery" }).click();
    const edit = page.getByRole("dialog", { name: "Edit gallery" });
    await edit.getByRole("button", { name: "Remove settings.png from the gallery" }).click();
    await edit.getByRole("button", { name: "Save" }).click();
    await expect(inEditor.locator("[data-gallery-picture]")).toHaveCount(2);
    await expect(page.locator("[data-draft-status]")).toHaveAttribute("data-draft-status", "saved");
    await publishFromEditor(page);
    await expect(shown(page).getByRole("list", { name: "Gallery of 2 pictures" })).toBeVisible();
  });

  test("anybody reading the public space sees the gallery's pictures but not one of a page they may not read", async ({ api, browser, freshOrg }, testInfo) => {
    test.slow();
    const { key, space, target, login, settings } = await screensPage(api, testInfo, made);
    const alice = must(await api.GET("/auth/me")).user;
    const secret = await createPage(api, space.homePageId, "Unreleased");
    must(
      await api.PUT("/pages/{pageID}/restrictions", { params: { path: { pageID: secret.id } }, body: { view: [{ type: "user", id: alice.id }], edit: [] } }),
    );
    const hidden = await upload(api, secret.id, "unreleased.png", "image/png", png(PICTURE_WIDTH, PICTURE_HEIGHT, [90, 90, 90]));
    await writeBody(api, target.id, [
      paragraph("What changed on screen."),
      { type: "gallery", attrs: { columns: 3 }, content: [picture(login, "The new sign-in"), picture(hidden, "Not yet"), picture(settings, null)] },
    ]);
    must(await api.PUT("/org/anonymous-access", { body: { enabled: true, indexable: false } }));
    must(await api.PUT("/spaces/{spaceKey}/anonymous-access", { params: { path: { spaceKey: key } }, body: { view: true } }));
    const context = await browser.newContext({ baseURL: WEB_URL, storageState: ANONYMOUS });
    try {
      const reader = await context.newPage();
      const gallery = shown(reader).getByRole("list", { name: "Gallery of 2 pictures" });
      // The reader is not alice, so a replica may not have her page or her switches yet.
      await openUntil(reader, `/public/${freshOrg.slug}/s/${key}/p/${target.id}/release-screens`, () => expect(gallery).toBeVisible(ONE_LOOK));
      await expect(gallery).not.toContainText("Not yet");
      const through = new RegExp(`/api/v1/public/${freshOrg.slug}/attachments/${login}\\?inline=1$`);
      await expect(gallery.locator("img").first()).toHaveAttribute("src", through);
      await gallery.getByRole("button", { name: "View picture larger, 2 of 2" }).click();
      await expect(position(reader)).toHaveText("2 of 2");
      await reader.keyboard.press("ArrowRight");
      await expect(lightbox(reader)).toHaveAccessibleName("The new sign-in");
      await expectAccessible(reader);
    } finally {
      await context.close();
      must(await api.PUT("/org/anonymous-access", { body: { enabled: false, indexable: false } }));
    }
  });

  for (const scheme of ["light", "dark"] as ColourScheme[]) {
    test(`a gallery and its lightbox pass axe in ${scheme}`, async ({ page, api }, testInfo) => {
      await startInScheme(page, scheme);
      const { target, login, settings, path } = await screensPage(api, testInfo, made);
      await writeBody(api, target.id, [
        paragraph("What changed on screen."),
        { type: "gallery", attrs: { columns: 4 }, content: [picture(login, "The new sign-in"), picture(settings, "Settings")] },
      ]);
      const gallery = shown(page).getByRole("list", { name: "Gallery of 2 pictures" });
      await openShowing(page, path, gallery);
      await expectAccessible(page);
      await gallery.getByRole("button", { name: "View Settings larger, 2 of 2" }).click();
      await expect(lightbox(page)).toHaveAccessibleName("Settings");
      await expectAccessible(page);
      await page.keyboard.press("Escape");

      await openShowing(page, `${path}/edit`, editorBox(page).locator("[data-gallery-node]"));
      await editorBox(page).getByRole("button", { name: "Edit gallery" }).click();
      await expect(page.getByRole("dialog", { name: "Edit gallery" }).getByLabel("Caption for settings.png")).toHaveValue("Settings");
      await expectAccessible(page);
    });
  }
});
