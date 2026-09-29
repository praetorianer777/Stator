import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { API_BASE, ME_STALE_MS } from "@/config";
import { HANDLES_UNAUTHORIZED } from "@/lib/session";
import { api } from "./client";
import type { components } from "./schema";

type Wire = components["schemas"];

/** Who is signed in, where they act now, and where else they may. */
export type Me = Wire["MeResponse"];
export type OrgRole = Wire["CurrentOrg"]["role"];
export type Provider = Wire["Provider"];
export type ProviderView = Wire["ProviderView"];
export type SaveProviderInput = Wire["SaveOIDCProviderRequest"];

export type JoinRequest = Wire["JoinRequest"];

export const meQueryKey = ["auth", "me"] as const;
export const providerQueryKey = ["oidc-provider"] as const;
export const joinRequestsQueryKey = ["users", "requests"] as const;

/** The session query, never retried: a 401 is a definite answer, which the route guards act on themselves. */
export const meQuery = {
  queryKey: meQueryKey,
  queryFn: async (): Promise<Me> => (await api.GET("/auth/me")).data!,
  staleTime: ME_STALE_MS,
  retry: false,
  meta: HANDLES_UNAUTHORIZED,
} as const;

export function useMe() {
  return useQuery(meQuery);
}

/** Whether a role may change the organization's settings. */
export function administers(role: OrgRole | undefined): boolean {
  return role === "owner" || role === "admin";
}

/** Where the browser goes to sign in through an organization's provider; a navigation, since it follows redirects. */
export function ssoStartURL(orgSlug: string, next?: string): string {
  const start = `${API_BASE}/auth/oidc/${encodeURIComponent(orgSlug.trim().toLowerCase())}/start`;
  return next ? `${start}?next=${encodeURIComponent(next)}` : start;
}

export function useLogin() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (body: { email: string; password: string }): Promise<Me> => (await api.POST("/auth/login", { body })).data!,
    onSuccess: (me) => queryClient.setQueryData(meQueryKey, me),
    // A 401 here is a wrong password, which the form shows.
    meta: HANDLES_UNAUTHORIZED,
  });
}

export function useLogout() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async () => {
      await api.POST("/auth/logout");
    },
    // Whatever was cached belonged to the person leaving.
    onSettled: () => queryClient.clear(),
  });
}

export function useProvider() {
  return useQuery({
    queryKey: providerQueryKey,
    queryFn: async (): Promise<ProviderView> => (await api.GET("/oidc-provider")).data!,
  });
}

/** Who signed in through the provider and waits to be let in; asked only of administrators. */
export function useJoinRequests(enabled = true) {
  return useQuery({
    queryKey: joinRequestsQueryKey,
    queryFn: async (): Promise<JoinRequest[]> => (await api.GET("/users/requests")).data!.requests,
    enabled,
  });
}

export function useAdmitJoinRequest() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async ({ userId, role }: { userId: string; role: "member" | "admin" }) =>
      (await api.POST("/users/requests/{userID}/admit", { params: { path: { userID: userId } }, body: { role } })).data!,
    onSettled: () => queryClient.invalidateQueries({ queryKey: joinRequestsQueryKey }),
  });
}

export function useDeclineJoinRequest() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async ({ userId }: { userId: string }) => {
      await api.DELETE("/users/requests/{userID}", { params: { path: { userID: userId } } });
    },
    onSettled: () => queryClient.invalidateQueries({ queryKey: joinRequestsQueryKey }),
  });
}

export function useSaveProvider() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (body: SaveProviderInput): Promise<ProviderView> => (await api.PUT("/oidc-provider", { body })).data!,
    onSuccess: (view) => queryClient.setQueryData(providerQueryKey, view),
  });
}
