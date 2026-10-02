import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { SHORTCUT_URL_SCHEMES } from "@/config";
import { api } from "./client";
import type { components } from "./schema";
import { treeQueryKey } from "./tree";

/** A link pinned above a space's page tree: a page the reader may view, or an address. */
export type Shortcut = components["schemas"]["Shortcut"];
/** A new shortcut: a page or an address, and a label. */
export type NewShortcut = components["schemas"]["ShortcutInput"];

// Under the trees' key: whatever changes which pages a reader sees in a tree
// (a trash, a restore, a restriction, an archive) changes which shortcuts they
// see too, and every such change already reads the trees again.
export const shortcutsQueryKey = [...treeQueryKey, "shortcuts"] as const;

export function spaceShortcutsQueryKey(spaceKey: string) {
  return [...shortcutsQueryKey, spaceKey.toUpperCase()] as const;
}

export function useShortcuts(spaceKey: string) {
  return useQuery({
    queryKey: spaceShortcutsQueryKey(spaceKey),
    queryFn: async (): Promise<Shortcut[]> => (await api.GET("/spaces/{spaceKey}/shortcuts", { params: { path: { spaceKey } } })).data!.shortcuts,
    enabled: spaceKey !== "",
  });
}

/** What a shortcut is called: its label, or the page's title as it is now. */
export function shortcutName(shortcut: Shortcut): string {
  return shortcut.label || shortcut.page?.title || shortcut.url || "";
}

/**
 * The address a link shortcut opens, or undefined when it is not one a browser
 * opens as a web page. The API refuses any other, so this only guards against
 * a stored address that somehow is not.
 */
export function safeHref(url: string | null | undefined): string | undefined {
  if (!url) return undefined;
  try {
    return SHORTCUT_URL_SCHEMES.includes(new URL(url).protocol) ? url : undefined;
  } catch {
    return undefined;
  }
}

export function useAddShortcut(spaceKey: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (input: NewShortcut): Promise<Shortcut> =>
      (await api.POST("/spaces/{spaceKey}/shortcuts", { params: { path: { spaceKey } }, body: input })).data!.shortcut,
    onSuccess: () => queryClient.invalidateQueries({ queryKey: spaceShortcutsQueryKey(spaceKey) }),
  });
}

/** Puts a shortcut after another, or first when after is null; the list takes the order the API answers. */
export function useMoveShortcut(spaceKey: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async ({ id, after }: { id: string; after: string | null }): Promise<Shortcut[]> =>
      (await api.POST("/spaces/{spaceKey}/shortcuts/{shortcutID}/move", { params: { path: { spaceKey, shortcutID: id } }, body: { after } })).data!.shortcuts,
    onSuccess: (shortcuts) => queryClient.setQueryData(spaceShortcutsQueryKey(spaceKey), shortcuts),
  });
}

export function useRemoveShortcut(spaceKey: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (id: string) => {
      await api.DELETE("/spaces/{spaceKey}/shortcuts/{shortcutID}", { params: { path: { spaceKey, shortcutID: id } } });
      return id;
    },
    onSuccess: (id) => {
      queryClient.setQueryData<Shortcut[]>(spaceShortcutsQueryKey(spaceKey), (current) => current?.filter((each) => each.id !== id));
      return queryClient.invalidateQueries({ queryKey: spaceShortcutsQueryKey(spaceKey) });
    },
  });
}
