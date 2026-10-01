import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "./client";
import type { components } from "./schema";

type Wire = components["schemas"];

/** A space: its key, its name, its home page, and what the reader may do in it. */
export type Space = Wire["Space"];
export type NewSpace = Wire["CreateInput"];
export type SpaceChanges = Wire["UpdateInput"];

export const spacesQueryKey = ["spaces"] as const;

export function spaceQueryKey(key: string) {
  return [...spacesQueryKey, key.toUpperCase()] as const;
}

/** The spaces the reader may see; archived ones only when asked for. */
export function useSpaces(includeArchived = false) {
  return useQuery({
    queryKey: includeArchived ? [...spacesQueryKey, { archived: true }] : spacesQueryKey,
    queryFn: async (): Promise<Space[]> => (await api.GET("/spaces", { params: { query: includeArchived ? { archived: true } : {} } })).data!.spaces,
  });
}

export function spaceQuery(key: string) {
  return {
    queryKey: spaceQueryKey(key),
    queryFn: async (): Promise<Space> => (await api.GET("/spaces/{spaceKey}", { params: { path: { spaceKey: key } } })).data!.space,
  };
}

export function useSpace(key: string) {
  return useQuery({ ...spaceQuery(key), enabled: Boolean(key) });
}

export function useCreateSpace() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (body: NewSpace): Promise<Space> => (await api.POST("/spaces", { body })).data!.space,
    onSuccess: (made) => {
      queryClient.setQueryData(spaceQueryKey(made.key), made);
      return queryClient.invalidateQueries({ queryKey: spacesQueryKey, exact: true });
    },
  });
}

export function useUpdateSpace(key: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (body: SpaceChanges): Promise<Space> =>
      (await api.PATCH("/spaces/{spaceKey}", { params: { path: { spaceKey: key } }, body })).data!.space,
    onSuccess: (updated) => {
      queryClient.setQueryData(spaceQueryKey(updated.key), updated);
      return queryClient.invalidateQueries({ queryKey: spacesQueryKey, exact: true });
    },
  });
}

export function useDeleteSpace(key: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async () => {
      await api.DELETE("/spaces/{spaceKey}", { params: { path: { spaceKey: key } } });
    },
    onSuccess: () => {
      queryClient.removeQueries({ queryKey: spaceQueryKey(key) });
      return queryClient.invalidateQueries({ queryKey: spacesQueryKey, exact: true });
    },
  });
}

/** Where a key comes from when nobody typed one: initials of several words, the start of one. Mirrors the API's SuggestKey. */
export function suggestKey(name: string, maxLength: number): string {
  const words = name
    .toUpperCase()
    .split(/[^\p{L}\p{N}]+/u)
    .map((word) => word.replace(/[^A-Z0-9]/g, ""))
    .filter(Boolean);
  if (words.length === 0) return "";
  let key = words.length === 1 ? words[0]! : words.map((word) => word[0]).join("");
  key = key.slice(0, maxLength);
  if (!/^[A-Z]/.test(key)) return "";
  return key.length === 1 ? key + key : key;
}
