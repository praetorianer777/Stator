import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { Outlet, RouterProvider, createMemoryHistory, createRootRoute, createRoute, createRouter } from "@tanstack/react-router";
import { TEMPLATE_BUTTON_LABEL_MAX_LENGTH, TEMPLATE_BUTTON_TITLE_MAX_LENGTH } from "@/config";
import { DocPageContext } from "@/features/editor/BlockViews";
import { stubApi } from "@/test/app";
import { axeViolations } from "@/test/axe";
import { allowlist } from "@/test/allowlist";
import { TemplateButton } from "./TemplateButton";
import { TemplateButtonDialog } from "./TemplateButtonDialog";
import { buttonTarget, buttonTitle, templateButtonSettings } from "./button";

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

const home = "0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a10";
const plans = "0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a11";
const made = "0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a12";

// The page the button sits on, and the editor's route it opens a new page in.
function shown(children: ReactNode) {
  const root = createRootRoute({
    component: () => (
      <main>
        <h1>Team</h1>
        <DocPageContext value={{ id: home, spaceKey: "TEAM" }}>
          <Outlet />
        </DocPageContext>
      </main>
    ),
  });
  const page = createRoute({ getParentRoute: () => root, path: "/", component: () => children });
  const editing = createRoute({ getParentRoute: () => root, path: "/s/$spaceKey/p/$pageId/$slug/edit", component: () => <p>Editing the new page</p> });
  const router = createRouter({ routeTree: root.addChildren([page, editing]), history: createMemoryHistory({ initialEntries: ["/"] }) });
  render(
    <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
      <RouterProvider router={router} />
    </QueryClientProvider>,
  );
  return router;
}

function target(over: Record<string, unknown> = {}) {
  return {
    template: { key: "meeting-notes", name: "Meeting notes", title: "Meeting notes {date}", variables: [] },
    spaceKey: "TEAM",
    spaceName: "Team",
    parent: { id: home, title: "Team", home: true },
    canCreate: true,
    ...over,
  };
}

describe("template button settings", () => {
  it("put right what the server would refuse, as the allowlist says", () => {
    expect(templateButtonSettings({ template: "Meeting Notes", space: "team", parent: "plans", label: 3, title: "x".repeat(400) })).toEqual({
      template: "",
      space: null,
      parent: null,
      label: "",
      title: "x".repeat(TEMPLATE_BUTTON_TITLE_MAX_LENGTH),
    });
    const kept = { template: "meeting-notes", space: "TEAM", parent: plans, label: "New notes", title: "Notes {date}" };
    expect(templateButtonSettings(kept)).toEqual(kept);
    const attrs = allowlist.nodes.templateButton?.attrs;
    expect(attrs?.label?.maxLength).toBe(TEMPLATE_BUTTON_LABEL_MAX_LENGTH);
    expect(attrs?.title?.maxLength).toBe(TEMPLATE_BUTTON_TITLE_MAX_LENGTH);
    expect(new RegExp(attrs!.template!.pattern!).test("meeting-notes")).toBe(true);
  });

  it("go to the page they name, else the top of their space or the page's own", () => {
    const settings = templateButtonSettings({ template: "how-to" });
    expect(buttonTarget(settings, "TEAM")).toEqual({ template: "how-to", spaceKey: "TEAM", parentId: null });
    expect(buttonTarget({ ...settings, space: "OPS", parent: plans }, "TEAM")).toEqual({ template: "how-to", spaceKey: "OPS", parentId: plans });
  });

  it("name the page by the button, else the template, on the reader's own day", () => {
    const now = new Date(2026, 9, 5, 23, 30);
    const settings = templateButtonSettings({ template: "meeting-notes", title: "Weekly {date}" });
    expect(buttonTitle(settings, target() as never, now)).toBe("Weekly 2026-10-05");
    expect(buttonTitle({ ...settings, title: " " }, target() as never, now)).toBe("Meeting notes 2026-10-05");
    // The server names a page from a template without a title after the template.
    expect(buttonTitle({ ...settings, title: "" }, target({ template: { key: "how-to", name: "How-to guide", title: "" } }) as never, now)).toBe("");
  });
});

describe("the template button", () => {
  it("makes a page from its template where it says, and opens it to edit", async () => {
    const user = userEvent.setup();
    const asked: string[] = [];
    const sent = stubApi({
      "GET /template-button": (request) => {
        asked.push(new URL(request.url).search);
        return { status: 200, body: target({ parent: { id: plans, title: "Plans", home: false } }) };
      },
      "POST /templates/meeting-notes/pages": { status: 201, body: { page: { id: made, title: "Weekly", spaceKey: "TEAM" } } },
    });
    const router = shown(
      <TemplateButton settings={templateButtonSettings({ template: "meeting-notes", parent: plans, label: "New notes", title: "Weekly" })} />,
    );
    const button = await screen.findByRole("button", { name: "New notes" });
    await waitFor(() => expect(button).toBeEnabled());
    expect(asked[0]).toBe(`?template=meeting-notes&spaceKey=TEAM&parentId=${plans}`);
    expect(button).toHaveAccessibleDescription("Makes a page from Meeting notes under Plans and opens it for you to write.");
    expect(await axeViolations()).toEqual([]);
    await user.click(button);
    expect(await screen.findByText("Editing the new page")).toBeInTheDocument();
    expect(sent.find((each) => each.method === "POST")?.body).toEqual({ parentId: plans, spaceKey: "TEAM", title: "Weekly" });
    expect(router.state.location.pathname).toBe(`/s/TEAM/p/${made}/weekly/edit`);
  });

  it("asks for its template's variables first, and sends them with the page", async () => {
    const user = userEvent.setup();
    const kickoff = "0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a13";
    const variables = [
      { name: "customer", label: "Customer", kind: "text", options: [], default: "", required: true },
      { name: "phase", label: "Phase", kind: "select", options: ["Discovery", "Delivery"], default: "Discovery", required: false },
    ];
    const sent = stubApi({
      "GET /template-button": { status: 200, body: target({ template: { key: kickoff, name: "Kick-off", title: "Kick-off {customer}", variables } }) },
      [`POST /templates/${kickoff}/pages`]: { status: 201, body: { page: { id: made, title: "Kick-off Acme", spaceKey: "TEAM" } } },
    });
    shown(<TemplateButton settings={templateButtonSettings({ template: kickoff })} />);
    const button = await screen.findByRole("button", { name: "New page: Kick-off" });
    await waitFor(() => expect(button).toBeEnabled());
    expect(button).toHaveAccessibleDescription("Asks what Kick-off needs, makes the page at the top of Team and opens it for you to write.");
    await user.click(button);
    const dialog = await screen.findByRole("dialog", { name: "New page from Kick-off" });
    await user.click(within(dialog).getByRole("button", { name: "Create page" }));
    expect(within(dialog).getByRole("alert")).toHaveTextContent("Fill in Customer first; the template needs it.");
    expect(sent.some((each) => each.method === "POST")).toBe(false);
    await user.type(within(dialog).getByLabelText("Customer (required)"), "Acme");
    expect(await axeViolations()).toEqual([]);
    await user.click(within(dialog).getByRole("button", { name: "Create page" }));
    expect(await screen.findByText("Editing the new page")).toBeInTheDocument();
    expect(sent.find((each) => each.method === "POST")?.body).toEqual({
      spaceKey: "TEAM",
      title: "Kick-off {customer}",
      values: { customer: "Acme", phase: "Discovery" },
    });
  });

  it("is disabled, with a sentence, for a reader who may not add pages there", async () => {
    stubApi({ "GET /template-button": { status: 200, body: target({ canCreate: false }) } });
    shown(<TemplateButton settings={templateButtonSettings({ template: "meeting-notes" })} />);
    const button = await screen.findByRole("button", { name: "New page: Meeting notes" });
    await waitFor(() => expect(button.closest("[data-template-button]")).toHaveAttribute("data-state", "refused"));
    expect(button).toBeDisabled();
    expect(button).toHaveAccessibleDescription("You may not add pages at the top of Team. Ask an administrator of the space to let you add pages.");
  });

  it("says when its template or place is gone, and does nothing in the editor", async () => {
    stubApi({ "GET /template-button": { status: 404, body: { error: { code: "not_found", message: "That page was not found." } } } });
    shown(<TemplateButton settings={templateButtonSettings({ template: "meeting-notes", parent: plans })} />);
    expect(await screen.findByText(/The template or the place for its page is gone/)).toBeInTheDocument();
    expect(screen.getByRole("button")).toBeDisabled();
    cleanup();
    stubApi({ "GET /template-button": { status: 200, body: target() } });
    shown(<TemplateButton settings={templateButtonSettings({ template: "meeting-notes" })} inEditor />);
    expect(await screen.findByText(/does nothing while you edit/)).toBeInTheDocument();
    expect(screen.getByRole("button")).toBeDisabled();
  });

  it("shows why a click failed, in the server's words", async () => {
    const user = userEvent.setup();
    stubApi({
      "GET /template-button": { status: 200, body: target() },
      "POST /templates/meeting-notes/pages": {
        status: 403,
        body: { error: { code: "forbidden", message: "You may not add pages here. Ask an administrator." } },
      },
    });
    shown(<TemplateButton settings={templateButtonSettings({ template: "meeting-notes" })} />);
    const button = await screen.findByRole("button", { name: "New page: Meeting notes" });
    await waitFor(() => expect(button).toBeEnabled());
    await user.click(button);
    expect(await screen.findByRole("alert")).toHaveTextContent("You may not add pages here. Ask an administrator.");
  });
});

describe("the template button dialog", () => {
  it("asks for a template, a place, the button's words and the page's title", async () => {
    stubApi({
      "GET /templates": {
        status: 200,
        body: { templates: [{ key: "meeting-notes", name: "Meeting notes", description: "", title: "", body: { type: "doc" }, builtIn: true }] },
      },
      "GET /spaces": { status: 200, body: { spaces: [{ key: "TEAM", name: "Team" }] } },
      "GET /spaces/TEAM/outline": {
        status: 200,
        body: {
          pages: [
            { id: home, parentId: null, title: "Team", depth: 0 },
            { id: plans, parentId: home, title: "Plans", depth: 1 },
          ],
        },
      },
    });
    const user = userEvent.setup();
    const onSave = vi.fn();
    shown(<TemplateButtonDialog initial={templateButtonSettings({})} pageSpace="TEAM" isNew onSave={onSave} onClose={() => {}} />);
    const dialog = await screen.findByRole("dialog", { name: "Insert a template button" });
    await user.click(within(dialog).getByRole("button", { name: "Insert" }));
    expect(within(dialog).getByText("Choose the template the button makes pages from.")).toBeInTheDocument();
    expect(onSave).not.toHaveBeenCalled();
    await within(dialog).findByRole("option", { name: "Meeting notes" });
    await within(dialog).findByRole("option", { name: "Plans" });
    await user.selectOptions(within(dialog).getByLabelText("Template"), "meeting-notes");
    await user.selectOptions(within(dialog).getByLabelText("Put new pages"), plans);
    await user.type(within(dialog).getByLabelText("Button text"), " New notes ");
    await user.type(within(dialog).getByLabelText("Title of each new page"), "Notes {{date}");
    expect(await axeViolations({ popupOpen: true })).toEqual([]);
    await user.click(within(dialog).getByRole("button", { name: "Insert" }));
    expect(onSave).toHaveBeenCalledWith({ template: "meeting-notes", space: null, parent: plans, label: "New notes", title: "Notes {date}" });
  });
});
