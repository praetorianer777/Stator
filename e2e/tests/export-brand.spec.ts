import { expect } from "../fixtures/auth";
import { teamTest as test } from "../fixtures/team";
import { expectAccessible, openAdminPage } from "../fixtures/shell";

// A 1 by 1 PNG, which is all a logo needs to be for the page to draw it.
const LOGO = Buffer.from("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNkYPhfDwAChwGA60e6kgAAAABJRU5ErkJggg==", "base64");

test.describe("the installation and the brand of its exports", { tag: ["@auth", "@desktop"] }, () => {
  test("an administrator sets a logo and footer lines, the sidebar shows the logo to everybody, and a member cannot change them", async ({ page, pageAs }) => {
    await page.goto("/");
    await openAdminPage(page, "installation");
    await expect(page.locator("[data-installation-name-shown]")).not.toBeEmpty();
    await expect(page.getByText("There is no logo yet.", { exact: false })).toBeVisible();
    await expectAccessible(page);

    await page.locator("[data-brand-logo-input]").setInputFiles({ name: "logo.png", mimeType: "image/png", buffer: LOGO });
    await expect(page.locator("[data-brand-logo]")).toBeVisible();
    await page.getByLabel("Footer line in English").fill("Internal use only");
    await page.getByLabel("Footer line in German").fill("Nur für den internen Gebrauch");
    await page.getByRole("button", { name: "Save footer" }).click();
    // The button is still enabled for a moment after the click, so only the notice proves the save landed before the reload.
    await expect(page.getByRole("status").filter({ hasText: "The footer line is saved." })).toBeVisible();

    await page.reload();
    await expect(page.getByLabel("Footer line in English")).toHaveValue("Internal use only");
    await expect(page.getByLabel("Footer line in German")).toHaveValue("Nur für den internen Gebrauch");
    await expect(page.locator("[data-sidebar] [data-site-logo]")).toBeVisible();
    await expect(page.locator("[data-sidebar] [data-logo-mark]")).toHaveCount(0);

    const bob = await pageAs("bob");
    await bob.goto("/spaces");
    await expect(bob.locator("[data-sidebar] [data-site-logo]")).toBeVisible();
    await bob.goto("/admin/installation");
    await expect(bob.getByText("Only administrators can open this page")).toBeVisible();

    await page.getByRole("button", { name: "Remove logo" }).click();
    await expect(page.getByText("There is no logo yet.", { exact: false })).toBeVisible();
    await page.goto("/spaces");
    await expect(page.locator("[data-sidebar] [data-logo-mark]")).toBeVisible();
  });

  test("a logo that is not a picture is refused in a sentence", async ({ page }) => {
    await page.goto("/admin/installation");
    await page.locator("[data-brand-logo-input]").setInputFiles({ name: "logo.png", mimeType: "image/png", buffer: Buffer.from("not a picture") });
    await expect(page.getByText("a PNG, JPEG or WebP picture", { exact: false }).first()).toBeVisible();
  });
});
