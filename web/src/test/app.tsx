import { render, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { RouterProvider, createMemoryHistory } from "@tanstack/react-router";
import { buildRouter } from "@/routes";

/** The real router and the real shell, started at a path; nothing below the shell is stood in for. */
export async function renderAt(path: string) {
  const queryClient = new QueryClient();
  const router = buildRouter(queryClient, createMemoryHistory({ initialEntries: [path] }));
  render(
    <QueryClientProvider client={queryClient}>
      <RouterProvider router={router} />
    </QueryClientProvider>,
  );
  await waitFor(() => expect(document.querySelector("[data-top-bar]")).not.toBeNull());
  return router;
}

/** Sets the viewport's width the way a browser window would change it. */
export function setViewportWidth(width: number) {
  Object.defineProperty(window, "innerWidth", { configurable: true, writable: true, value: width });
  window.dispatchEvent(new Event("resize"));
}
