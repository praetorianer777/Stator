import { afterEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { renderAt, signedIn, stubApi } from "@/test/app";
import { axeViolations } from "@/test/axe";

afterEach(() => vi.unstubAllGlobals());

const member = { ...signedIn, organization: { ...signedIn.organization!, role: "member" as const } };
const empty = { logo: null, footer: { en: "", de: "" } };

describe("the brand of the exports", () => {
  it("lets an administrator set the footer in both languages", async () => {
    const sent = stubApi({
      "GET /org/brand": { status: 200, body: { brand: empty } },
      "PUT /org/brand/footer": { status: 200, body: { brand: { logo: null, footer: { en: "Internal", de: "Intern" } } } },
    });
    await renderAt("/settings/brand");
    expect(await screen.findByText(/There is no logo yet/)).toBeInTheDocument();
    await userEvent.type(screen.getByLabelText("Footer line in English"), "Internal");
    await userEvent.type(screen.getByLabelText("Footer line in German"), "Intern");
    await userEvent.click(screen.getByRole("button", { name: "Save footer" }));
    await waitFor(() => expect(sent.find((r) => r.method === "PUT")?.body).toEqual({ en: "Internal", de: "Intern" }));
    expect(await axeViolations()).toEqual([]);
  });

  it("shows the logo it has, with a way to replace and remove it", async () => {
    stubApi({ "GET /org/brand": { status: 200, body: { brand: { logo: { contentType: "image/png", size: 100, version: 3 }, footer: { en: "", de: "" } } } } });
    await renderAt("/settings/brand");
    const logo = await screen.findByAltText("The organization's logo");
    expect(logo).toHaveAttribute("src", expect.stringContaining("/org/brand/logo?v=3"));
    expect(screen.getByRole("button", { name: "Replace logo" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Remove logo" })).toBeInTheDocument();
  });

  it("refuses a logo over the limit before sending it", async () => {
    const sent = stubApi({ "GET /org/brand": { status: 200, body: { brand: empty } } });
    await renderAt("/settings/brand");
    await screen.findByText(/There is no logo yet/);
    const big = new File([new Uint8Array(3 * 1024 * 1024)], "big.png", { type: "image/png" });
    await userEvent.upload(screen.getByLabelText("Choose a logo picture"), big);
    expect(await screen.findByText(/That logo is too large/)).toBeInTheDocument();
    expect(sent.some((r) => r.method === "PUT")).toBe(false);
  });

  it("tells a member who changes it", async () => {
    stubApi({ "GET /org/brand": { status: 200, body: { brand: empty } } });
    await renderAt("/settings/brand", { me: member });
    expect(await screen.findByText(/Only an administrator of the organization changes the brand/)).toBeInTheDocument();
  });
});
