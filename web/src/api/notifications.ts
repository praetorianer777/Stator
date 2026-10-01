import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { NOTIFICATION_PANEL_SIZE, UNREAD_POLL_MS } from "@/config";
import { api } from "./client";
import type { components } from "./schema";

type Wire = components["schemas"];

/** One thing the caller was told; the client words it from its kind, actor and page. */
export type Notification = Wire["Notification"];
export type NotificationKind = Wire["Notification"]["kind"];
/** How the caller wants to be told. */
export type Preferences = Wire["Preferences"];
export type Switches = Wire["Switches"];
export type Digest = Wire["Preferences"]["digest"];

/** Every kind, in the order the preferences list them, as the API's notify.Kinds. */
export const NOTIFICATION_KINDS: NotificationKind[] = ["mentioned", "shared", "replied", "commented", "resolved", "published", "created", "expired"];
export const DIGESTS: Digest[] = ["off", "hourly", "daily"];

export const notificationsQueryKey = ["notifications"] as const;
const unreadQueryKey = [...notificationsQueryKey, "unread"] as const;
const preferencesQueryKey = ["notification-preferences"] as const;

/** How many are unread, asked every UNREAD_POLL_MS and whenever the window regains focus. */
export function useUnreadCount() {
  return useQuery({
    queryKey: unreadQueryKey,
    queryFn: async (): Promise<number> => (await api.GET("/notifications/unread-count")).data!.unread,
    refetchInterval: UNREAD_POLL_MS,
    refetchOnWindowFocus: "always",
  });
}

/** The latest notifications, read when the panel opens. */
export function useNotifications(enabled: boolean) {
  return useQuery({
    queryKey: [...notificationsQueryKey, "list"],
    queryFn: async () => (await api.GET("/notifications", { params: { query: { limit: NOTIFICATION_PANEL_SIZE } } })).data!,
    enabled,
    staleTime: 0,
  });
}

/** Marks the named notifications read, or all of them, and the badge with them. */
export function useMarkRead() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (which: { ids: string[] } | { all: true }) => {
      await api.POST("/notifications/read", { body: which });
    },
    onSuccess: () => queryClient.invalidateQueries({ queryKey: notificationsQueryKey }),
  });
}

export function usePreferences() {
  return useQuery({
    queryKey: preferencesQueryKey,
    queryFn: async (): Promise<Preferences> => (await api.GET("/notification-preferences")).data!.preferences,
  });
}

export function useSavePreferences() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (preferences: Preferences): Promise<Preferences> => (await api.PUT("/notification-preferences", { body: preferences })).data!.preferences,
    onSuccess: (saved) => queryClient.setQueryData(preferencesQueryKey, saved),
  });
}
