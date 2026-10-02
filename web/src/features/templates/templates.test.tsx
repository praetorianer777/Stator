import "@/routes/templates.lazy";
import { afterEach, describe, expect, it, vi } from "vitest";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Editor } from "@tiptap/core";
import type { Template } from "@/api/templates";
import { DocView } from "@/features/editor/DocView";
import { editorExtensions } from "@/features/editor/extensions";
import { today } from "@/features/editor/InlineValueViews";
import type { DocNode } from "@/features/editor/schema";
import { allowlist, problems, templateAllowlist } from "@/test/allowlist";
import { arrival, renderAt, stubApi, type Answer } from "@/test/app";
import { axeViolations } from "@/test/axe";
import { aPage, aSpace } from "@/test/spaces";
import { nameFrom, variablesOf } from "./TemplateEditor";
import { initialValues, missingValues, wireValues } from "./TemplateValues";

afterEach(() => vi.unstubAllGlobals());

const space = aSpace();
const home = aPage();
const made = aPage({ id: "0195f000-0000-7000-8000-0000000000d1", title: "Kickoff with Acme", home: false, parentId: home.id });
const bob = { id: "0195f000-0000-7000-8000-00000000b0b0", name: "Bob Builder", email: "bob@stator.test", canView: true };
const carl = { id: "0195f000-0000-7000-8000-00000000c0c0", name: "Carl Closed", email: "carl@stator.test", canView: false };

const kickoff: Template = {
  key: "0195f000-0000-7000-8000-0000000007e1",
  name: "Kickoff",
  description: "Who we start with.",
  title: "Kickoff with {customer}",
  builtIn: false,
  scope: "space",
  spaceKey: "DOCS",
  canEdit: true,
  body: {
    type: "doc",
    content: [
      {
        type: "paragraph",
        content: [
          { type: "text", text: "Customer: " },
          { type: "templateVariable", attrs: { name: "customer" } },
        ],
      },
      {
        type: "paragraph",
        content: [
          { type: "text", text: "Owner: " },
          { type: "templateVariable", attrs: { name: "owner" } },
        ],
      },
    ],
  },
  variables: [
    { name: "customer", label: "Customer", kind: "text", options: [], default: "", required: true },
    { name: "day", label: "Kickoff day", kind: "date", options: [], default: "today", required: false },
    { name: "stage", label: "Stage", kind: "select", options: ["Lead", "Won"], default: "Lead", required: false },
    { name: "owner", label: "Owner", kind: "person", options: [], default: "", required: false },
  ],
};
const company: Template = {
  ...kickoff,
  key: "0195f000-0000-7000-8000-0000000007e2",
  name: "Company kickoff",
  scope: "organization",
  spaceKey: "",
  variables: [],
};

function stubDialog(more: Record<string, Answer> = {}) {
  return stubApi({
    "GET /spaces": { status: 200, body: { spaces: [space] } },
    "GET /spaces/DOCS": { status: 200, body: { space } },
    [`GET /pages/${home.id}`]: { status: 200, body: { page: home, space } },
    "GET /spaces/DOCS/pages": { status: 200, body: { pages: [] } },
    "GET /templates": { status: 200, body: { templates: [kickoff, company] } },
    [`GET /pages/${home.id}/mentionable`]: { status: 200, body: { people: [carl, bob] } },
    "POST /pages": { status: 201, body: { page: made } },
    ...more,
  });
}

async function chooseKickoff() {
  await userEvent.click(await screen.findByRole("button", { name: "New page" }));
  const dialog = await screen.findByRole("dialog", { name: "New page under Handbook" });
  await userEvent.click(await within(dialog).findByRole("radio", { name: /^Kickoff/ }));
  return dialog;
}

describe("the template editor against the server's template allowlist", () => {
  it("has exactly the nodes a template may hold, and its blanks only there", () => {
    const e = new Editor({ element: document.createElement("div"), extensions: editorExtensions({ variant: "template" }) });
    for (const [name, type] of Object.entries(e.schema.nodes)) {
      expect(templateAllowlist.nodes, `node ${name}`).toHaveProperty([name]);
      expect(Object.keys(type.spec.attrs ?? {}).sort(), `attributes of ${name}`).toEqual(Object.keys(templateAllowlist.nodes[name]?.attrs ?? {}).sort());
    }
    expect(Object.keys(templateAllowlist.nodes).sort()).toEqual(Object.keys(e.schema.nodes).sort());
    e.chain().focus("end").insertContent("For ").insertVariable("customer").run();
    expect(e.chain().insertVariable("Not A Name").run()).toBe(false);
    const doc = e.getJSON() as DocNode;
    expect(JSON.stringify(doc)).toContain('{"type":"templateVariable","attrs":{"name":"customer"}}');
    expect(problems(doc, undefined, undefined, templateAllowlist)).toEqual([]);
    expect(problems(doc)).not.toEqual([]);
    e.destroy();

    const page = new Editor({ element: document.createElement("div"), extensions: editorExtensions() });
    expect(page.schema.nodes.templateVariable).toBeUndefined();
    expect(allowlist.nodes.templateVariable).toBeUndefined();
    page.destroy();
  });

  it("draws a blank in a preview as its name in braces", () => {
    render(<DocView doc={kickoff.body} anchors={false} />);
    const blank = document.querySelector("[data-template-variable=customer]");
    expect(blank).toHaveTextContent("Blank: {customer}");
    expect(blank).toHaveClass("doc-variable");
  });
});

describe("a template's variables in the form", () => {
  it("names a variable after its label", () => {
    expect(nameFrom("Kick-off day")).toBe("kick_off_day");
    expect(nameFrom("Café owner")).toBe("cafe_owner");
    expect(nameFrom("1st call")).toBe("v_1st_call");
    expect(nameFrom("  ")).toBe("v");
  });

  it("starts from the defaults and sends what was given", () => {
    const values = initialValues(kickoff.variables);
    expect(values).toEqual({ customer: "", day: today(), stage: "Lead" });
    expect(missingValues(kickoff.variables, values)).toEqual(["Customer"]);
    const filled = { ...values, customer: " Acme ", owner: { id: bob.id, name: bob.name } };
    expect(missingValues(kickoff.variables, filled)).toEqual([]);
    expect(wireValues(filled)).toEqual({ customer: "Acme", day: today(), stage: "Lead", owner: bob.id });
  });

  it("sends choices only for a choice, and no default for a person", () => {
    const rows = [
      { key: 1, name: "stage", nameTyped: true, label: " Stage ", kind: "select" as const, options: "Lead\n\n Won ", default: "Lead", required: true },
      { key: 2, name: "owner", nameTyped: false, label: "Owner", kind: "person" as const, options: "x", default: "x", required: false },
    ];
    expect(variablesOf(rows)).toEqual([
      { name: "stage", label: "Stage", kind: "select", options: ["Lead", "Won"], default: "Lead", required: true },
      { name: "owner", label: "Owner", kind: "person", options: [], default: "", required: false },
    ]);
  });
});

describe("making a page from a template with variables", () => {
  it("asks for each variable and sends the values for the server to fill in", async () => {
    const sent = stubDialog();
    const router = await renderAt("/s/DOCS");
    const dialog = await chooseKickoff();
    expect(within(dialog).getByLabelText("Title")).toHaveValue("Kickoff with {customer}");
    const form = within(dialog).getByRole("group", { name: "Fill in the template" });
    expect(within(form).getByLabelText("Kickoff day")).toHaveValue(today());
    expect(within(form).getByLabelText("Stage")).toHaveValue("Lead");
    expect(within(dialog).getByRole("region", { name: "Preview of Kickoff" }).querySelector("[data-template-variable=owner]")).not.toBeNull();

    await userEvent.click(within(dialog).getByRole("button", { name: "Create page" }));
    expect(within(dialog).getByRole("alert")).toHaveTextContent("Fill in Customer first");
    expect(sent.some((r) => r.method === "POST" && r.path === "/pages")).toBe(false);

    await userEvent.type(within(form).getByLabelText("Customer (required)"), "Acme");
    await userEvent.selectOptions(within(form).getByLabelText("Stage"), "Won");
    await userEvent.type(within(form).getByRole("combobox", { name: "Owner" }), "B");
    expect(within(form).queryByRole("option", { name: /Carl Closed/ })).toBeNull();
    await userEvent.click(await within(form).findByRole("option", { name: /Bob Builder/ }));
    expect(within(form).getByText("Bob Builder")).toBeInTheDocument();
    expect(await axeViolations()).toEqual([]);

    await userEvent.click(within(dialog).getByRole("button", { name: "Create page" }));
    await arrival(router, `/s/DOCS/p/${made.id}/kickoff-with-acme/edit`);
    expect(sent.find((r) => r.method === "POST" && r.path === "/pages")?.body).toEqual({
      parentId: home.id,
      title: "Kickoff with {customer}",
      template: kickoff.key,
      values: { customer: "Acme", day: today(), stage: "Won", owner: bob.id },
    });
  });

  it("shows the server's sentence next to the value it refuses", async () => {
    stubDialog({
      "POST /pages": {
        status: 422,
        body: {
          error: {
            code: "validation_failed",
            message: "Some fields need attention.",
            fields: { "values.owner": "Owner has to be somebody who may view this space; pick another person." },
          },
        },
      },
    });
    await renderAt("/s/DOCS");
    const dialog = await chooseKickoff();
    await userEvent.type(within(dialog).getByLabelText("Customer (required)"), "Acme");
    await userEvent.click(within(dialog).getByRole("button", { name: "Create page" }));
    expect(await within(dialog).findByText("Owner has to be somebody who may view this space; pick another person.")).toBeInTheDocument();
    expect(within(dialog).queryByText("Some fields need attention.")).toBeNull();
  });
});

describe("keeping templates", () => {
  it("lists a space's templates in its settings and deletes one after asking", async () => {
    vi.stubGlobal("confirm", () => true);
    const sent = stubDialog({ [`DELETE /templates/${kickoff.key}`]: { status: 204 } });
    const router = await renderAt("/s/DOCS/settings?tab=templates");
    await arrival(router, "/s/DOCS/settings");
    const list = await screen.findByRole("region", { name: "This space's templates" });
    const row = await within(list).findByText("Kickoff");
    expect(within(list).queryByText("Company kickoff")).toBeNull();
    expect(row.closest("tr")).toHaveTextContent("4 variables");
    expect(await axeViolations()).toEqual([]);
    await userEvent.click(within(list).getByRole("button", { name: "Delete Kickoff" }));
    expect(await within(list).findByText("Deleted Kickoff.")).toBeInTheDocument();
    expect(sent.some((r) => r.method === "DELETE" && r.path === `/templates/${kickoff.key}`)).toBe(true);
  });

  it("makes a template with variables, puts their blanks in the body, and goes back to the space", async () => {
    const sent = stubDialog({ "POST /templates": { status: 201, body: { template: kickoff } } });
    const router = await renderAt("/settings/templates/new?space=DOCS");
    await arrival(router, "/settings/templates/new");
    const editor = await screen.findByRole("textbox", { name: "Body" });
    await userEvent.type(screen.getByLabelText("Name"), "Kickoff");
    await userEvent.type(screen.getByLabelText("Page title"), "Kickoff with {{customer}");
    await userEvent.click(screen.getByRole("button", { name: "Add variable" }));
    const variables = screen.getByRole("group", { name: "Variables" });
    await userEvent.type(within(variables).getByLabelText("Label"), "Customer");
    expect(within(variables).getByLabelText("Name")).toHaveValue("customer");
    await userEvent.click(within(variables).getByLabelText("Required"));
    await userEvent.click(within(variables).getByRole("button", { name: "Add variable" }));
    const second = within(variables).getAllByRole("listitem")[1]!;
    await userEvent.type(within(second).getByLabelText("Label"), "Stage");
    await userEvent.selectOptions(within(second).getByLabelText("Kind"), "select");
    await userEvent.type(within(second).getByLabelText("Choices"), "Lead{Enter}Won");
    await userEvent.selectOptions(within(second).getByLabelText("Default"), "Won");
    editor.focus();
    await userEvent.click(within(variables).getByRole("button", { name: "Insert Customer" }));
    await waitFor(() => expect(editor.querySelector("[data-template-variable=customer]")).not.toBeNull());
    expect(await axeViolations()).toEqual([]);

    await userEvent.click(screen.getByRole("button", { name: "Save template" }));
    await arrival(router, "/s/DOCS/settings");
    const body = sent.find((r) => r.method === "POST" && r.path === "/templates")?.body as Record<string, unknown>;
    expect(body).toMatchObject({
      spaceKey: "DOCS",
      name: "Kickoff",
      title: "Kickoff with {customer}",
      variables: [
        { name: "customer", label: "Customer", kind: "text", options: [], default: "", required: true },
        { name: "stage", label: "Stage", kind: "select", options: ["Lead", "Won"], default: "Won", required: false },
      ],
    });
    expect(JSON.stringify(body.body)).toContain('{"type":"templateVariable","attrs":{"name":"customer"}}');
  });

  it("refuses a built-in, saying why", async () => {
    stubDialog({
      "GET /templates/meeting-notes": {
        status: 200,
        body: { template: { ...company, key: "meeting-notes", builtIn: true, scope: "builtIn", canEdit: false } },
      },
    });
    const router = await renderAt("/settings/templates/meeting-notes");
    await arrival(router, "/settings/templates/meeting-notes");
    expect(await screen.findByText(/Built-in templates come with Stator and cannot be changed/)).toBeInTheDocument();
  });
});
