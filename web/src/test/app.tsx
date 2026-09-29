import { render, waitFor } from "@testing-library/react";
import { QueryClientProvider } from "@tanstack/react-query";
import { RouterProvider, createMemoryHistory } from "@tanstack/react-router";
import { vi } from "vitest";
import { meQueryKey, type Me } from "@/api/auth";
import { createQueryClient } from "@/lib/session";
import { buildRouter, sendToLogin } from "@/routes";

/** The person every shell test is signed in as unless it says otherwise: an administrator. */
export const signedIn: Me = {
  user: { id: "u-ada", email: "ada@stator.test", name: "Ada Lovelace" },
  organization: { id: "o-demo", slug: "demo", name: "Demo", role: "admin" },
  organizations: [{ orgId: "o-demo", orgSlug: "demo", orgName: "Demo", role: "admin" }],
};

/**
 * The real router and the real shell, started at a path, signed in as `me`; null
 * is nobody, and then the page is wherever the router sends a visitor.
 */
export async function renderAt(path: string, { me = signedIn }: { me?: Me | null } = {}) {
  const queryClient = createQueryClient((client) => sendToLogin(router, client));
  if (me) queryClient.setQueryData(meQueryKey, me);
  const router = buildRouter(queryClient, createMemoryHistory({ initialEntries: [path] }));
  render(
    <QueryClientProvider client={queryClient}>
      <RouterProvider router={router} />
    </QueryClientProvider>,
  );
  await waitFor(() => expect(document.querySelector("[data-top-bar], [data-login]")).not.toBeNull());
  return router;
}

/** One stubbed API answer: a status and a JSON body, for a method and a path under /api/v1. */
export type Answer = { status: number; body?: unknown };

/**
 * Stands in for the API by "METHOD /path", anything else a 404 in its envelope,
 * and returns the requests made so a test can say what was sent.
 */
export function stubApi(answers: Record<string, Answer | ((request: Request) => Answer)>) {
  const sent: { method: string; path: string; body: unknown }[] = [];
  vi.stubGlobal("fetch", async (input: Request) => {
    const url = new URL(input.url, "http://app.test");
    const path = url.pathname.replace(/^\/api\/v1/, "");
    const text = await input.clone().text();
    sent.push({ method: input.method, path, body: text ? JSON.parse(text) : undefined });
    const found = answers[`${input.method} ${path}`];
    const answer = typeof found === "function" ? found(input) : (found ?? { status: 404, body: { error: { code: "not_found", message: "Not stubbed." } } });
    return new Response(answer.body === undefined ? null : JSON.stringify(answer.body), {
      status: answer.status,
      headers: { "Content-Type": "application/json" },
    });
  });
  return sent;
}

/** A 401 in the API's envelope. */
export const unauthorized: Answer = { status: 401, body: { error: { code: "unauthorized", message: "Sign in to continue." } } };

/** Sets the viewport's width the way a browser window would change it. */
export function setViewportWidth(width: number) {
  Object.defineProperty(window, "innerWidth", { configurable: true, writable: true, value: width });
  window.dispatchEvent(new Event("resize"));
}
