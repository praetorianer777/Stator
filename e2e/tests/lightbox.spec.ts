import { crc32, deflateSync } from "node:zlib";
import type { Page, TestInfo } from "@playwright/test";
import { must, type StatorApi } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { orgTest as test } from "../fixtures/org";
import { ONE_LOOK, openShowing, openUntil } from "../fixtures/replica";
import { uniqueName } from "../fixtures/seed";
import { expectAccessible, scrollsSideways, startInScheme, type ColourScheme } from "../fixtures/shell";
import { createPage, createSpace, deleteSpace, uniqueKey } from "../fixtures/spaces";
import { WEB_URL } from "../fixtures/stack";

const ANONYMOUS = { cookies: [], origins: [] };
// Larger than any window, so a zoomed picture overhangs it and pans.
const PICTURE_WIDTH = 2400;
const PICTURE_HEIGHT = 1800;
// Long enough for the recorder to write a few frames, short enough to cost nothing.
const RECORDING_MS = 600;
const RECORDING_FPS = 25;
// HTMLMediaElement.HAVE_CURRENT_DATA: the player has a frame to show.
const HAVE_CURRENT_DATA = 2;

/** A PNG of one colour, made here so the suite carries no binary files. */
function png(width: number, height: number, [r, g, b]: [number, number, number]): Buffer {
  const row = Buffer.alloc(1 + width * 3);
  for (let x = 0; x < width; x++) row.set([r, g, b], 1 + x * 3);
  const raw = Buffer.concat(Array.from({ length: height }, () => row));
  const chunk = (type: string, data: Buffer) => {
    const length = Buffer.alloc(4);
    length.writeUInt32BE(data.length);
    const body = Buffer.concat([Buffer.from(type, "ascii"), data]);
    const sum = Buffer.alloc(4);
    sum.writeUInt32BE(crc32(body));
    return Buffer.concat([length, body, sum]);
  };
  const header = Buffer.alloc(13);
  header.writeUInt32BE(width, 0);
  header.writeUInt32BE(height, 4);
  header.set([8, 2, 0, 0, 0], 8);
  return Buffer.concat([
    Buffer.from([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a]),
    chunk("IHDR", header),
    chunk("IDAT", deflateSync(raw)),
    chunk("IEND", Buffer.alloc(0)),
  ]);
}

/** A short WebM the browser records from a canvas, since the suite carries no binary files. */
async function webm(page: Page): Promise<Buffer> {
  const base64 = await page.evaluate(
    async ({ ms, fps }) => {
      const canvas = document.createElement("canvas");
      canvas.width = 64;
      canvas.height = 48;
      const context = canvas.getContext("2d")!;
      const recorder = new MediaRecorder(canvas.captureStream(fps), { mimeType: "video/webm" });
      const parts: Blob[] = [];
      recorder.ondataavailable = (event) => parts.push(event.data);
      let frame = 0;
      const timer = setInterval(() => {
        context.fillStyle = frame++ % 2 ? "#3366cc" : "#cc6633";
        context.fillRect(0, 0, canvas.width, canvas.height);
      }, 1000 / fps);
      const stopped = new Promise((resolve) => (recorder.onstop = resolve));
      recorder.start();
      await new Promise((resolve) => setTimeout(resolve, ms));
      recorder.stop();
      await stopped;
      clearInterval(timer);
      const bytes = new Uint8Array(await new Blob(parts, { type: "video/webm" }).arrayBuffer());
      let binary = "";
      for (const byte of bytes) binary += String.fromCharCode(byte);
      return btoa(binary);
    },
    { ms: RECORDING_MS, fps: RECORDING_FPS },
  );
  return Buffer.from(base64, "base64");
}

async function upload(api: StatorApi, pageId: string, name: string, type: string, bytes: Buffer): Promise<string> {
  const form = new FormData();
  form.append("file", new Blob([new Uint8Array(bytes)], { type }), name);
  // The generated type describes the multipart fields as strings; the form itself is what is sent.
  const body = form as unknown as { file: string };
  return must(await api.POST("/pages/{pageID}/attachments", { params: { path: { pageID: pageId } }, body })).attachment.id;
}

const lightbox = (page: Page) => page.getByRole("dialog");
const zoom = (page: Page) => lightbox(page).locator("[data-lightbox-image]");
const picture = (page: Page) => page.locator("main [data-doc]").getByRole("button", { name: "View The harbour at dawn larger" });

/** The harbour page: a picture in its words, a video named in a line, and three more files below. */
async function harbourPage(api: StatorApi, page: Page, testInfo: TestInfo) {
  const key = uniqueKey(testInfo);
  const space = await createSpace(api, key, uniqueName(testInfo, "Harbour"));
  const target = await createPage(api, space.homePageId, "Harbour tour");
  const harbour = await upload(api, target.id, "harbour.png", "image/png", png(PICTURE_WIDTH, PICTURE_HEIGHT, [30, 90, 160]));
  const lighthouse = await upload(api, target.id, "lighthouse.png", "image/png", png(PICTURE_HEIGHT, PICTURE_WIDTH, [200, 60, 40]));
  await upload(api, target.id, "notes.txt", "text/plain", Buffer.from("Moorings are free after six."));
  const clip = await upload(api, target.id, "tour.webm", "video/webm", await webm(page));
  must(
    await api.PATCH("/pages/{pageID}", {
      params: { path: { pageID: target.id } },
      body: {
        version: 1,
        body: {
          type: "doc",
          content: [
            { type: "paragraph", content: [{ type: "text", text: "The harbour from the pier." }] },
            { type: "image", attrs: { attachmentId: harbour, alt: "The harbour at dawn", width: null } },
            {
              type: "paragraph",
              content: [
                { type: "text", text: "Watch the tour: " },
                { type: "attachment", attrs: { attachmentId: clip, fileName: "tour.webm" } },
              ],
            },
          ],
        },
      },
    }),
  );
  return { key, target, harbour, lighthouse, clip, path: `/s/${key}/p/${target.id}/harbour-tour` };
}

/** Plays the video until the browser has asked for a stretch of it and has its first frame. */
async function playsByRanges(page: Page, clip: string, opened: () => Promise<void>) {
  const ranged = page.waitForResponse((res) => res.url().includes(`/attachments/${clip}`) && res.status() === 206);
  await opened();
  const player = lightbox(page).locator("video");
  await expect(player).toBeVisible();
  const answer = await ranged;
  expect(answer.headers()["content-range"]).toMatch(/^bytes \d+-\d+\/\d+$/);
  expect(answer.headers()["content-type"]).toBe("video/webm");
  await expectPlaying(page);
}

/** The lightbox's player has a frame of the video to show, and no sentence says it cannot. */
async function expectPlaying(page: Page) {
  const player = lightbox(page).locator("video");
  await expect.poll(() => player.evaluate((video: HTMLVideoElement) => video.readyState)).toBeGreaterThanOrEqual(HAVE_CURRENT_DATA);
  await expect(lightbox(page).getByRole("alert")).toHaveCount(0);
}

test.describe("attachment lightbox", { tag: ["@auth"] }, () => {
  const made: string[] = [];
  test.afterEach(async ({ api }) => {
    for (const key of made.splice(0)) await deleteSpace(api, key);
  });

  test("a reader opens a page's picture larger, zooms it, steps through its files and plays its video", async ({ page, api }) => {
    test.slow();
    await page.goto("/");
    const { key, harbour, clip, path } = await harbourPage(api, page, test.info());
    made.push(key);
    await openShowing(page, path, picture(page));

    await picture(page).click();
    await expect(lightbox(page)).toHaveAccessibleName("The harbour at dawn");
    await expect(zoom(page)).toHaveAttribute("src", `/api/v1/attachments/${harbour}?inline=1`);
    await expect.poll(() => zoom(page).evaluate((img: HTMLImageElement) => img.naturalWidth)).toBe(PICTURE_WIDTH);
    expect(await scrollsSideways(page)).toBe(false);
    const box = await lightbox(page).boundingBox();
    const viewport = page.viewportSize()!;
    expect(box).toMatchObject({ x: 0, y: 0, width: viewport.width, height: viewport.height });

    await lightbox(page).getByRole("button", { name: "Zoom in" }).click();
    await expect(zoom(page)).toHaveAttribute("data-zoom", "150");
    await page.keyboard.press("+");
    await expect(zoom(page)).toHaveAttribute("data-zoom", "225");
    await page.keyboard.press("0");
    await expect(zoom(page)).toHaveAttribute("data-zoom", "100");
    const stage = lightbox(page).locator("[data-lightbox-stage]");
    const middle = await stage.boundingBox();
    await page.mouse.move(middle!.x + middle!.width / 2, middle!.y + middle!.height / 2);
    await page.mouse.wheel(0, -400);
    await expect.poll(async () => Number(await zoom(page).getAttribute("data-zoom"))).toBeGreaterThan(100);
    // An arrow key moves a zoomed picture rather than going to the next.
    const before = await zoom(page).evaluate((img) => getComputedStyle(img).transform);
    await page.keyboard.press("ArrowLeft");
    await expect.poll(() => zoom(page).evaluate((img) => getComputedStyle(img).transform)).not.toBe(before);
    await page.keyboard.press("Escape");
    await expect(lightbox(page)).toHaveCount(0);
    await expect(picture(page)).toBeFocused();

    // From the files below the page, the lightbox steps through its pictures and its video.
    const files = page.locator("[data-attachments]");
    await files.getByRole("button", { name: "Preview harbour.png" }).click();
    await expect(lightbox(page)).toHaveAccessibleName("harbour.png");
    await expect(lightbox(page).locator("[data-lightbox-position]")).toHaveText("3 of 3");
    await expect(files.getByRole("button", { name: "Preview notes.txt" })).toHaveCount(0);
    await lightbox(page).getByRole("button", { name: "Previous" }).click();
    await expect(lightbox(page)).toHaveAccessibleName("lighthouse.png");
    await playsByRanges(page, clip, () => lightbox(page).getByRole("button", { name: "Previous" }).click());
    await expect(lightbox(page)).toHaveAccessibleName("tour.webm");
    await expect(lightbox(page).getByRole("button", { name: "Zoom in" })).toHaveCount(0);
    await lightbox(page).getByRole("button", { name: "Close" }).click();

    // The video named in the page's words plays where it is named, from what the browser keeps of it.
    await page.locator("main [data-doc]").getByRole("button", { name: "Play tour.webm" }).click();
    await expect(lightbox(page)).toHaveAccessibleName("tour.webm");
    await expectPlaying(page);
    await page.keyboard.press("Escape");
    await expect(lightbox(page)).toHaveCount(0);
  });

  test("somebody reading through a public link opens the picture and plays the video through the link", async ({ page, api, browser }) => {
    test.slow();
    await page.goto("/");
    const { key, target, harbour, clip, lighthouse } = await harbourPage(api, page, test.info());
    made.push(key);
    const link = must(await api.POST("/pages/{pageID}/public-links", { params: { path: { pageID: target.id } }, body: {} }));
    const context = await browser.newContext({ baseURL: WEB_URL, storageState: ANONYMOUS });
    try {
      const reader = await context.newPage();
      await openUntil(reader, link.path, () => expect(picture(reader)).toBeVisible(ONE_LOOK));
      await picture(reader).click();
      const through = new RegExp(`/api/v1/public/[^/]+/links/[^/]+/attachments/${harbour}\\?inline=1$`);
      await expect(zoom(reader)).toHaveAttribute("src", through);
      await expect.poll(() => zoom(reader).evaluate((img: HTMLImageElement) => img.naturalWidth)).toBe(PICTURE_WIDTH);
      await reader.keyboard.press("Escape");
      await playsByRanges(reader, clip, () => reader.locator("main [data-doc]").getByRole("button", { name: "Play tour.webm" }).click());

      // A stretch of the page's other files comes through the link as well.
      const src = (await reader.locator("main [data-doc] img").getAttribute("src"))!;
      const beside = await context.request.get(src.replace(harbour, lighthouse).replace("?inline=1", ""), { headers: { Range: "bytes=0-7" } });
      expect(beside.status()).toBe(206);
      expect(beside.headers()["content-range"]).toMatch(/^bytes 0-7\/\d+$/);
      expect((await beside.body()).subarray(1, 4).toString()).toBe("PNG");
    } finally {
      await context.close();
    }
  });

  for (const scheme of ["light", "dark"] as ColourScheme[]) {
    test(`the lightbox passes axe in ${scheme}, zoomed and playing`, async ({ page, api }) => {
      test.slow();
      await startInScheme(page, scheme);
      await page.goto("/");
      const { key, clip, path } = await harbourPage(api, page, test.info());
      made.push(key);
      await openShowing(page, path, picture(page));
      await picture(page).click();
      await expect.poll(() => zoom(page).evaluate((img: HTMLImageElement) => img.naturalWidth)).toBe(PICTURE_WIDTH);
      await page.keyboard.press("+");
      await expect(zoom(page)).toHaveAttribute("data-zoom", "150");
      await expectAccessible(page);
      await page.keyboard.press("Escape");
      await playsByRanges(page, clip, () => page.locator("[data-attachments]").getByRole("button", { name: "Preview tour.webm" }).click());
      await expect(lightbox(page).locator("[data-lightbox-position]")).toHaveText("1 of 3");
      await expectAccessible(page);
    });
  }
});
