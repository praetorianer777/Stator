import { MutationCache, QueryCache, QueryClient } from "@tanstack/react-query";
import { ApiError } from "@/api/client";
import { accessQueryKey, isNoAccess, type GlobalCan } from "@/api/permissions";
import { LOGIN_PATH, QUERY_NETWORK_RETRIES, QUERY_NETWORK_RETRY_MS, QUERY_STALE_MS } from "@/config";
import { t } from "@/i18n";

/** Marks a query or mutation that answers a 401 itself, such as asking who is signed in. */
export const HANDLES_UNAUTHORIZED = { handlesUnauthorized: true } as const;

type Listener = (message: string) => void;

const listeners = new WeakMap<QueryClient, Set<Listener>>();

/**
 * The application's query client. A 401 from any request means the session is gone, and onUnauthorized says what follows;
 * a 403 no_access means the organization does not let the caller in, which the shell reads from the access query.
 * Any other failed change its caller does not show goes to the listeners onUnreportedMutationError adds.
 */
export function createQueryClient(onUnauthorized: (client: QueryClient) => void): QueryClient {
  const onError = (error: unknown, meta: Record<string, unknown> | undefined) => {
    if (error instanceof ApiError && error.isUnauthenticated && !meta?.handlesUnauthorized) onUnauthorized(client);
    if (isNoAccess(error)) client.setQueryData<GlobalCan>(accessQueryKey, { use: false, createSpace: false, administer: false });
  };
  const client: QueryClient = new QueryClient({
    queryCache: new QueryCache({ onError: (error, query) => onError(error, query.meta) }),
    mutationCache: new MutationCache({
      onError: (error, _vars, _ctx, mutation) => {
        onError(error, mutation.meta);
        // Signing in again or the no-access page says what happened; a toast would only repeat it.
        if (mutation.meta?.handlesError || (error instanceof ApiError && error.isUnauthenticated) || isNoAccess(error)) return;
        const message = error instanceof ApiError && error.message ? error.message : t.api.unsaved;
        listeners.get(client)?.forEach((listener) => listener(message));
      },
    }),
    defaultOptions: {
      queries: {
        // A failed request is usually a real answer (401, 404, validation), not
        // a blip worth retrying while the user waits. One that got no answer at
        // all, such as the connection dropping as the network changes, is asked
        // again: left alone it would stand for a wrong answer until a reload.
        retry: (failures, error) => !(error instanceof ApiError) && failures < QUERY_NETWORK_RETRIES,
        retryDelay: QUERY_NETWORK_RETRY_MS,
        refetchOnWindowFocus: false,
        staleTime: QUERY_STALE_MS,
      },
    },
  });
  return client;
}

/** Hears every failed change nobody else reported; returns how to stop. */
export function onUnreportedMutationError(client: QueryClient, listener: Listener): () => void {
  const set = listeners.get(client) ?? new Set<Listener>();
  listeners.set(client, set);
  set.add(listener);
  return () => {
    set.delete(listener);
  };
}

/** A path of this application to return to, never another site and never the sign-in page itself. */
export function safeNext(next: string | undefined): string | undefined {
  if (!next || !next.startsWith("/") || next.startsWith("//") || next.startsWith("/\\")) return undefined;
  return next === LOGIN_PATH || next.startsWith(`${LOGIN_PATH}?`) ? undefined : next;
}
