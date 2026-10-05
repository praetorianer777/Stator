import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { membersQueryKey } from "./auth";
import { api } from "./client";
import type { components } from "./schema";

type Wire = components["schemas"];

/** Somebody from outside let into one space, with what they may do there. */
export type Guest = Wire["Guest"];
export type GuestRole = Guest["role"];
export type InvitableRole = Wire["GuestInviteInput"]["role"];

/** The roles an invitation offers, from the least to the most. */
export const INVITABLE_ROLES: InvitableRole[] = ["viewer", "commenter", "editor"];

export function guestsQueryKey(spaceKey: string) {
  return ["space-guests", spaceKey] as const;
}

/** The guests of a space; asked only of the organization's administrators. */
export function useGuests(spaceKey: string, enabled: boolean) {
  return useQuery({
    queryKey: guestsQueryKey(spaceKey),
    queryFn: async (): Promise<Guest[]> => (await api.GET("/spaces/{spaceKey}/guests", { params: { path: { spaceKey } } })).data!.guests,
    enabled,
  });
}

export function useInviteGuest(spaceKey: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (body: { email: string; role: InvitableRole }): Promise<Guest> =>
      (await api.POST("/spaces/{spaceKey}/guests", { params: { path: { spaceKey } }, body })).data!.guest,
    onSettled: () =>
      Promise.all([queryClient.invalidateQueries({ queryKey: guestsQueryKey(spaceKey) }), queryClient.invalidateQueries({ queryKey: membersQueryKey })]),
  });
}

export function useRemoveGuest(spaceKey: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async ({ userId }: { userId: string }) => {
      await api.DELETE("/spaces/{spaceKey}/guests/{userID}", { params: { path: { spaceKey, userID: userId } } });
    },
    onSettled: () =>
      Promise.all([queryClient.invalidateQueries({ queryKey: guestsQueryKey(spaceKey) }), queryClient.invalidateQueries({ queryKey: membersQueryKey })]),
  });
}
