import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { Me } from "@/api/auth";
import { PROFILE_PATH } from "@/config";
import { applyLanguage } from "@/i18n";
import { de } from "@/i18n/de";
import { en } from "@/i18n/en";
import { renderAt, signedIn, stubApi } from "@/test/app";

beforeEach(() => localStorage.clear());
afterEach(() => {
  vi.unstubAllGlobals();
  applyLanguage("");
});

const withChoice = (locale: Me["user"]["locale"]): Me => ({ ...signedIn, user: { ...signedIn.user, locale } });

describe("the interface language", () => {
  it("follows the browser while the person has not chosen", async () => {
    stubApi({});
    await renderAt("/");
    expect(document.documentElement.lang).toBe("en");
    expect(screen.getByRole("button", { name: en.account.menu })).toBeInTheDocument();
  });

  it("speaks the language the profile chose", async () => {
    stubApi({});
    await renderAt("/", { me: withChoice("de") });
    await waitFor(() => expect(screen.getByRole("button", { name: de.account.menu })).toBeInTheDocument());
    expect(document.documentElement.lang).toBe("de");
  });

  it("is chosen in the profile, saved, and shown at once", async () => {
    const sent = stubApi({ "PATCH /auth/me": { status: 200, body: withChoice("de") } });
    await renderAt(PROFILE_PATH);
    await userEvent.selectOptions(await screen.findByLabelText(en.profile.languageField), "de");

    await waitFor(() => expect(screen.getByRole("heading", { level: 1, name: de.profile.title })).toBeInTheDocument());
    expect(sent.find((each) => each.method === "PATCH")).toEqual({ method: "PATCH", path: "/auth/me", body: { locale: "de" } });
    expect(screen.getByLabelText(de.profile.languageField)).toHaveValue("de");
    expect(document.querySelector("[data-language-saved]")).toHaveTextContent(de.profile.languageSaved);
    expect(screen.getByText(de.profile.languageBoundary)).toBeInTheDocument();
  });

  it("goes back to the browser's language when the choice is cleared", async () => {
    stubApi({ "PATCH /auth/me": { status: 200, body: withChoice("") } });
    await renderAt(PROFILE_PATH, { me: withChoice("de") });
    await userEvent.selectOptions(await screen.findByLabelText(de.profile.languageField), "");

    await waitFor(() => expect(screen.getByRole("heading", { level: 1, name: en.profile.title })).toBeInTheDocument());
    expect(document.documentElement.lang).toBe("en");
  });
});
