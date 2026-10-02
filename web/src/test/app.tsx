import { render, waitFor } from "@testing-library/react";
import { QueryClientProvider } from "@tanstack/react-query";
import { createMemoryHistory } from "@tanstack/react-router";
import { onTestFinished, vi } from "vitest";
import { meQueryKey, type Me } from "@/api/auth";
import { LocalizedRouter } from "@/features/shell/LocalizedRouter";
import { createQueryClient } from "@/lib/session";
import { buildRouter, sendToLogin } from "@/routes";

/** The person every shell test is signed in as unless it says otherwise: an administrator. */
export const signedIn: Me = {
  user: { id: "u-ada", email: "ada@stator.test", name: "Ada Lovelace", locale: "", showInReaders: true },
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
  // The router draws nothing until a lazy route's chunk has arrived, which on a
  // busy machine outlasts any polling timeout; awaiting the load has none.
  await router.load();
  render(
    <QueryClientProvider client={queryClient}>
      <LocalizedRouter router={router} />
    </QueryClientProvider>,
  );
  await waitFor(() => expect(document.querySelector("[data-top-bar], [data-login]")).not.toBeNull());
  // A navigation still loading at the end of a test settles, or fires the
  // router's pending timer, after jsdom has gone, and fails a later file.
  onTestFinished(() => {
    if (router.state.status !== "idle") {
      throw new Error(`The test ended while the router was still loading ${router.state.location.pathname}. Await arrival(router, path) before it ends.`);
    }
  });
  return router;
}

/** Resolves once the router has finished loading `pathname`, lazy chunks and loaders included. */
export function arrival(router: ReturnType<typeof buildRouter>, pathname: string) {
  return new Promise<void>((resolve) => {
    const arrived = () => router.state.status === "idle" && router.state.resolvedLocation?.pathname === pathname;
    if (arrived()) return resolve();
    const stop = router.subscribe("onResolved", () => {
      if (!arrived()) return;
      stop();
      resolve();
    });
  });
}

/** One stubbed API answer: a status and a JSON body, for a method and a path under /api/v1. */
export type Answer = { status: number; body?: unknown };

/**
 * Stands in for the API by "METHOD /path", anything else a 404 in its envelope,
 * and returns the requests made so a test can say what was sent.
 */
export function stubApi(answers: Record<string, Answer | ((request: Request) => Answer | Promise<Answer>)>) {
  const sent: { method: string; path: string; body: unknown }[] = [];
  vi.stubGlobal("fetch", async (input: Request) => {
    const url = new URL(input.url, "http://app.test");
    const path = url.pathname.replace(/^\/api\/v1/, "");
    const text = await input.clone().text();
    sent.push({ method: input.method, path, body: text ? JSON.parse(text) : undefined });
    // Every page shows its comments; a test that does not speak of them means none.
    const found =
      answers[`${input.method} ${path}`] ??
      (input.method === "GET" && /^\/pages\/[^/]+\/comments$/.test(path) ? { status: 200, body: { threads: [] } } : undefined);
    const answer =
      typeof found === "function" ? await found(input) : (found ?? { status: 404, body: { error: { code: "not_found", message: "Not stubbed." } } });
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
