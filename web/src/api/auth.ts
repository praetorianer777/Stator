import { useEffect } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useMutation } from "./mutation";
import { API_BASE, ME_STALE_MS } from "@/config";
import { applyLanguage, type LanguageChoice } from "@/i18n";
import { applyTimeZone } from "@/lib/format";
import { HANDLES_UNAUTHORIZED } from "@/lib/session";
import { api } from "./client";
import type { components } from "./schema";

type Wire = components["schemas"];

/** Who is signed in, and their standing in the organization. */
export type Me = Wire["MeResponse"];
export type SiteRole = Wire["Standing"]["role"];
export type Provider = Wire["Provider"];
export type ProviderView = Wire["ProviderView"];
export type SaveProviderInput = Wire["SaveOIDCProviderRequest"];

export type JoinRequest = Wire["JoinRequest"];
export type Member = Wire["Member"];

export const meQueryKey = ["auth", "me"] as const;
export const providerQueryKey = ["oidc-provider"] as const;
export const membersQueryKey = ["users", "members"] as const;
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

/**
 * Follows the signed-in person's language and time zone whenever the app has
 * asked who they are; it never asks itself.
 */
export function useProfileLanguage(): void {
  const { data } = useQuery({ ...meQuery, enabled: false });
  const choice = data?.user.locale;
  const zone = data?.user.timezone;
  useEffect(() => {
    if (choice !== undefined) applyLanguage(choice);
  }, [choice]);
  useEffect(() => {
    if (zone !== undefined) applyTimeZone(zone);
  }, [zone]);
}

/** How the caller reads the interface: its language and the zone of its times; empty follows the browser. */
export interface ReadingChoice {
  locale: LanguageChoice;
  timezone: string;
}

/** Saves the caller's language and time zone together, as the profile's Save does. */
export function useSaveReading() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (choice: ReadingChoice): Promise<Me> => (await api.PATCH("/auth/me", { body: choice })).data!,
    onSuccess: (me) => queryClient.setQueryData(meQueryKey, me),
  });
}

/** The one space a guest belongs to and lands in; null for everybody else. */
export function guestSpaceOf(me: Me | undefined): Wire["SpaceRef"] | null {
  return me?.organization?.guestSpace ?? null;
}

/** Whether a grant lets the signed-in person administer the installation, as the session says. */
export function administers(me: Me | undefined): boolean {
  return me?.organization?.administers ?? false;
}

/** Where the browser goes to sign in through the identity provider; a navigation, since it follows redirects. */
export function ssoStartURL(next?: string): string {
  const start = `${API_BASE}/auth/oidc/start`;
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
    onSettled: () =>
      Promise.all([queryClient.invalidateQueries({ queryKey: joinRequestsQueryKey }), queryClient.invalidateQueries({ queryKey: membersQueryKey })]),
  });
}

/** The installation's members and guests, with the roles each holds across it; asked only of administrators. */
export function useMembers() {
  return useQuery({
    queryKey: membersQueryKey,
    queryFn: async (): Promise<Member[]> => (await api.GET("/users")).data!.members,
  });
}

export type CreatedMember = Wire["CreatedMember"];

/** Adds a person with a password; a password left out is made and comes back once. */
export function useCreateMember() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (body: { email: string; name: string; role: "member" | "admin"; password: string }): Promise<CreatedMember> =>
      (await api.POST("/users", { body })).data!.member,
    onSettled: () => queryClient.invalidateQueries({ queryKey: membersQueryKey }),
  });
}

/** Switches somebody's account off, so they cannot sign in, or back on. */
export function useSetMemberActive() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async ({ userId, active }: { userId: string; active: boolean }) => {
      await api.PATCH("/users/{userID}", { params: { path: { userID: userId } }, body: { active } });
    },
    onSettled: () => queryClient.invalidateQueries({ queryKey: membersQueryKey }),
  });
}

export function useRemoveMember() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async ({ userId }: { userId: string }) => {
      await api.DELETE("/users/{userID}", { params: { path: { userID: userId } } });
    },
    onSettled: () => queryClient.invalidateQueries({ queryKey: membersQueryKey }),
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
