import { LOGIN_PATH, authTest as test, expect } from "../fixtures/auth";
import { WEB_URL } from "../fixtures/stack";

const ANONYMOUS = { cookies: [], origins: [] };

test.describe("the logo and the icons", { tag: ["@auth", "@desktop"] }, () => {
  test("are served from the site's root, with the type of the file", async ({ browser }) => {
    const context = await browser.newContext({ baseURL: WEB_URL, storageState: ANONYMOUS });
    for (const [path, type] of [
      ["/favicon.ico", /image\/(x-icon|vnd\.microsoft\.icon)/],
      ["/favicon-32.png", /image\/png/],
      ["/apple-touch-icon.png", /image\/png/],
      ["/logo-256.webp", /image\/webp/],
      ["/mark-96.webp", /image\/webp/],
      ["/site.webmanifest", /(application\/manifest\+json|application\/json)/],
    ] as const) {
      const response = await context.request.get(path);
      expect(response.status(), path).toBe(200);
      expect(response.headers()["content-type"], path).toMatch(type);
    }
    await context.close();
  });

  test("show on the sign-in page, which the content security policy lets load", async ({ browser }) => {
    const context = await browser.newContext({ baseURL: WEB_URL, storageState: ANONYMOUS });
    const page = await context.newPage();
    const refused: string[] = [];
    page.on("console", (message) => {
      if (/Content Security Policy|Refused to load/i.test(message.text())) refused.push(message.text());
    });
    await page.goto(LOGIN_PATH);
    const logo = page.locator("[data-login] img[data-logo]");
    await expect(logo).toBeVisible();
    await expect.poll(() => logo.evaluate((img) => (img as HTMLImageElement).naturalWidth)).toBeGreaterThan(0);
    await expect(page.locator('link[rel="icon"]').first()).toHaveAttribute("href", "/favicon.ico");
    expect(refused).toEqual([]);
    await context.close();
  });
});
