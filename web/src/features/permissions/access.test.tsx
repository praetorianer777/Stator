import { afterEach, describe, expect, it, vi } from "vitest";
import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { AccessReport, AccessStep } from "@/api/permissions";
import { renderAt, stubApi, type Answer } from "@/test/app";
import { axeViolations } from "@/test/axe";
import { aPage, aSpace } from "@/test/spaces";
import { decidingStep, stepSentence } from "./AccessDialog";

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

const ids = {
  plans: "0195f000-0000-7000-8000-0000000000a1",
  secret: "0195f000-0000-7000-8000-0000000000a2",
  bob: "0195f000-0000-7000-8000-00000000b0b0",
  eng: "0195f000-0000-7000-8000-00000000e000",
};
const home = aPage();
const plans = { id: ids.plans, title: "Plans", home: false };
const secret = aPage({ id: ids.secret, title: "Secret", home: false, parentId: ids.plans, ancestors: [{ id: home.id, title: "Handbook", home: true }, plans] });
const everyone = { type: "everyone" as const, id: null, name: "Everyone" };
const eng = { type: "group" as const, id: ids.eng, name: "Engineering" };
const step = (over: Partial<AccessStep>): AccessStep => ({ kind: "view", passed: true, bypassed: false, via: [], grants: [], listed: [], ...over });

const bobsAccess: AccessReport = {
  person: { id: ids.bob, name: "Bob Builder", email: "bob@stator.test" },
  role: "member",
  roleSource: "manual",
  rights: [
    {
      right: "view",
      allowed: false,
      steps: [
        step({ kind: "use", via: [everyone] }),
        step({ kind: "space", permission: "view", grants: [{ subject: everyone, permissions: ["view", "addPages"] }] }),
        step({ kind: "list", list: "view", page: plans, passed: false, listed: [eng] }),
      ],
    },
    { right: "edit", allowed: false, steps: [step({ kind: "view", passed: false })] },
    { right: "delete", allowed: false, steps: [step({ kind: "view", passed: false })] },
    { right: "comment", allowed: false, steps: [step({ kind: "view", passed: false })] },
  ],
};

function stubPage(space = aSpace(), more: Record<string, Answer> = {}) {
  return stubApi({
    "GET /spaces": { status: 200, body: { spaces: [space] } },
    "GET /spaces/DOCS": { status: 200, body: { space } },
    "GET /spaces/DOCS/pages": { status: 200, body: { pages: [] } },
    [`GET /pages/${ids.secret}`]: { status: 200, body: { page: secret, space } },
    "GET /people": { status: 200, body: { people: [{ id: ids.bob, name: "Bob Builder", email: "bob@stator.test" }] } },
    ...more,
  });
}

async function openInspector() {
  await renderAt(`/s/DOCS/p/${ids.secret}/secret`);
  await userEvent.click(await screen.findByRole("button", { name: "Page actions" }));
  await userEvent.click(await screen.findByRole("menuitem", { name: "Check access" }));
  return screen.findByRole("dialog", { name: "Who may do what on Secret" });
}

describe("checking somebody's access", () => {
  it("shows each right and marks the restriction that decides a no", async () => {
    const sent = stubPage(aSpace(), { [`GET /pages/${ids.secret}/access/${ids.bob}`]: { status: 200, body: { access: bobsAccess } } });
    const dialog = await openInspector();
    await userEvent.type(within(dialog).getByRole("combobox", { name: "Person to check" }), "Bob");
    await userEvent.click(await within(dialog).findByRole("option", { name: /Bob Builder/ }));
    expect(sent.some((r) => r.path === "/groups")).toBe(false);

    const report = await within(dialog).findByRole("region", { name: "What Bob Builder may do" });
    const view = report.querySelector<HTMLElement>("[data-access-right=view]")!;
    expect(within(view).getByText("Not allowed")).toBeInTheDocument();
    const decides = view.querySelector<HTMLElement>("[data-decides]")!;
    expect(decides).toHaveTextContent("Not on the view list of Plans.");
    expect(decides).toHaveTextContent("This decides it");
    expect(within(decides).getByText("Engineering")).toBeInTheDocument();
    expect(within(view).getByText("Add pages", { exact: false })).toBeInTheDocument();
    const edit = report.querySelector<HTMLElement>("[data-access-right=edit]")!;
    expect(edit.querySelector("[data-decides]")).toHaveTextContent("May not view the page");
    expect(await axeViolations()).toEqual([]);
  });

  it("says why when the server refuses, in its own sentence", async () => {
    const refusal: Answer = {
      status: 403,
      body: { error: { code: "forbidden", message: "Only an administrator of this space can check what somebody may do here." } },
    };
    stubPage(aSpace(), { [`GET /pages/${ids.secret}/access/${ids.bob}`]: refusal });
    const dialog = await openInspector();
    await userEvent.type(within(dialog).getByRole("combobox", { name: "Person to check" }), "Bob");
    await userEvent.click(await within(dialog).findByRole("option", { name: /Bob Builder/ }));
    expect(await within(dialog).findByText(/Only an administrator of this space can check/)).toBeInTheDocument();
  });

  it("is not offered to somebody who does not administer the space", async () => {
    stubPage(aSpace({ can: { editPages: true, administer: false, delete: false, purgeTrash: false, addComments: true, deletePages: true } }));
    await renderAt(`/s/DOCS/p/${ids.secret}/secret`);
    await userEvent.click(await screen.findByRole("button", { name: "Page actions" }));
    await screen.findByRole("menuitem", { name: "Copy" });
    expect(screen.queryByRole("menuitem", { name: "Check access" })).toBeNull();
  });
});

describe("the sentences of the steps", () => {
  const member = { role: "member" as const, roleSource: "manual" as const };
  it("say where an administrator's standing comes from", () => {
    expect(stepSentence(step({ kind: "orgAdmin" }), { role: "admin", roleSource: "oidc" })).toMatch(
      /^Administers the organization.*identity provider's groups/,
    );
    expect(stepSentence(step({ kind: "orgAdmin" }), { role: "owner", roleSource: "manual" })).toMatch(/^Owns the organization/);
  });

  it("tell a list passed by being on it from one an administrator passes", () => {
    expect(stepSentence(step({ kind: "list", list: "edit", page: plans }), member)).toBe("On the edit list of Plans.");
    expect(stepSentence(step({ kind: "list", list: "edit", page: plans, bypassed: true }), member)).toMatch(
      /^Not on the edit list of Plans, but administrators/,
    );
    expect(stepSentence(step({ kind: "space", permission: "addPages", passed: false }), member)).toMatch(/^Does not hold the Add pages permission/);
  });

  it("find the step that decides, or none", () => {
    expect(decidingStep(bobsAccess.rights[0]!)).toBe(2);
    expect(decidingStep({ right: "view", allowed: true, steps: [step({})] })).toBe(-1);
  });
});
