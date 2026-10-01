import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { afterEach, describe, expect, it, vi } from "vitest";
import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { templateTitle, type Template } from "@/api/templates";
import { arrival, renderAt, stubApi } from "@/test/app";
import { axeViolations } from "@/test/axe";
import { aPage, aSpace } from "@/test/spaces";

afterEach(() => vi.unstubAllGlobals());

const templates = (
  JSON.parse(readFileSync(resolve(process.cwd(), "../backend/internal/template/builtin/en.json"), "utf8")) as { templates: Omit<Template, "builtIn">[] }
).templates.map((tpl) => ({ ...tpl, builtIn: true }));
const space = aSpace();
const home = aPage();
const made = aPage({ id: "0195f000-0000-7000-8000-0000000000d1", title: "Retro", home: false, parentId: home.id });

function stub() {
  return stubApi({
    "GET /spaces": { status: 200, body: { spaces: [space] } },
    "GET /spaces/DOCS": { status: 200, body: { space } },
    [`GET /pages/${home.id}`]: { status: 200, body: { page: home, space } },
    "GET /spaces/DOCS/pages": { status: 200, body: { pages: [] } },
    "GET /templates": { status: 200, body: { templates } },
    "POST /pages": { status: 201, body: { page: made } },
  });
}

async function openDialog() {
  await userEvent.click(await screen.findByRole("button", { name: "New page" }));
  const dialog = await screen.findByRole("dialog", { name: "New page under Handbook" });
  await within(dialog).findByRole("radio", { name: /^Project plan/ });
  return dialog;
}

const checked = (dialog: HTMLElement) => within(dialog).getByRole("radio", { checked: true });

describe("the template picker", () => {
  it("offers a blank page and every template, blank first and chosen", async () => {
    stub();
    await renderAt("/s/DOCS");
    const dialog = await openDialog();
    const group = within(dialog).getByRole("radiogroup", { name: "Start from" });
    expect(
      within(group)
        .getAllByRole("radio")
        .map((r) => r.getAttribute("data-template")),
    ).toEqual(["blank", ...templates.map((tpl) => tpl.key)]);
    expect(checked(dialog)).toHaveAttribute("data-template", "blank");
    expect(within(dialog).getByRole("region", { name: "Preview of Blank page" })).toHaveTextContent("The page starts empty.");
    expect(await axeViolations()).toEqual([]);
  });

  it("previews the chosen template read-only, hints and all, and names the page after it", async () => {
    stub();
    await renderAt("/s/DOCS");
    const dialog = await openDialog();
    await userEvent.click(within(dialog).getByRole("radio", { name: /^Meeting notes/ }));
    expect(within(dialog).getByLabelText("Title")).toHaveValue(templateTitle("Meeting notes {date}"));
    const preview = within(dialog).getByRole("region", { name: "Preview of Meeting notes" });
    expect(within(preview).getByRole("heading", { name: "Participants" })).toBeInTheDocument();
    expect(preview.querySelector("[data-hint]")).toHaveTextContent("When the meeting took place");
    expect(preview.querySelector("[contenteditable], button, a")).toBeNull();
    await userEvent.click(within(dialog).getByRole("radio", { name: /^How-to guide/ }));
    expect(within(dialog).getByLabelText("Title")).toHaveValue("");
    expect(await axeViolations()).toEqual([]);
  });

  it("is one stop for the keyboard, and the arrows move the choice", async () => {
    stub();
    await renderAt("/s/DOCS");
    const dialog = await openDialog();
    const radios = within(dialog).getAllByRole("radio");
    expect(radios.filter((r) => r.tabIndex === 0)).toHaveLength(1);
    radios[0]?.focus();
    await userEvent.keyboard("{ArrowDown}");
    expect(checked(dialog)).toHaveAttribute("data-template", "meeting-notes");
    expect(document.activeElement).toBe(checked(dialog));
    await userEvent.keyboard("{ArrowUp}{ArrowUp}");
    expect(checked(dialog)).toHaveAttribute("data-template", "project-plan");
    await userEvent.keyboard("{Home}");
    expect(checked(dialog)).toHaveAttribute("data-template", "blank");
    await userEvent.keyboard("{End}");
    expect(checked(dialog)).toHaveAttribute("data-template", "project-plan");
    expect(
      within(dialog)
        .getAllByRole("radio")
        .filter((r) => r.tabIndex === 0),
    ).toEqual([checked(dialog)]);
  });

  it("makes the page unpublished with the template's body, keeps a typed title, and opens the editor", async () => {
    const sent = stub();
    const router = await renderAt("/s/DOCS");
    const dialog = await openDialog();
    await userEvent.type(within(dialog).getByLabelText("Title"), "Retro");
    await userEvent.click(within(dialog).getByRole("radio", { name: /^Retrospective/ }));
    expect(within(dialog).getByLabelText("Title")).toHaveValue("Retro");
    await userEvent.click(within(dialog).getByRole("button", { name: "Create page" }));
    await arrival(router, `/s/DOCS/p/${made.id}/retro/edit`);
    const retrospective = templates.find((tpl) => tpl.key === "retrospective");
    expect(sent.find((r) => r.method === "POST" && r.path === "/pages")?.body).toEqual({ parentId: home.id, title: "Retro", body: retrospective?.body });
  });

  it("still makes a blank page when the templates cannot be loaded", async () => {
    const sent = stubApi({
      "GET /spaces": { status: 200, body: { spaces: [space] } },
      "GET /spaces/DOCS": { status: 200, body: { space } },
      [`GET /pages/${home.id}`]: { status: 200, body: { page: home, space } },
      "GET /spaces/DOCS/pages": { status: 200, body: { pages: [] } },
      "POST /pages": { status: 201, body: { page: made } },
    });
    const router = await renderAt("/s/DOCS");
    await userEvent.click(await screen.findByRole("button", { name: "New page" }));
    const dialog = await screen.findByRole("dialog", { name: "New page under Handbook" });
    expect(await within(dialog).findByText(/The templates could not be loaded/)).toBeInTheDocument();
    await userEvent.type(within(dialog).getByLabelText("Title"), "Retro");
    await userEvent.click(within(dialog).getByRole("button", { name: "Create page" }));
    await arrival(router, `/s/DOCS/p/${made.id}/retro/edit`);
    expect(sent.find((r) => r.method === "POST" && r.path === "/pages")?.body).toEqual({ parentId: home.id, title: "Retro" });
  });
});

describe("a template's title", () => {
  it("takes the local day for the date token", () => {
    expect(templateTitle("Meeting notes {date}", new Date(2026, 0, 5, 23, 30))).toBe("Meeting notes 2026-01-05");
    expect(templateTitle("Plain")).toBe("Plain");
  });
});
