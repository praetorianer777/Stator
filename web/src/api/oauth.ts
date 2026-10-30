import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useMutation } from "./mutation";
import { api } from "./client";
import type { components } from "./schema";

type Wire = components["schemas"];

/** What the consent page shows for an app's authorize query (#449). */
export type Consent = Wire["Consent"];
export type ConsentAnswer = Wire["ConsentAnswer"];
export type ConsentDecision = Wire["ConsentRequest"];

export const consentQueryKey = (query: string) => ["oauth", "consent", query] as const;

/** The consent for the query exactly as the app sent it, which the API reads whole. */
export function useConsent(query: string) {
  return useQuery({
    queryKey: consentQueryKey(query),
    queryFn: async (): Promise<ConsentAnswer> => (await api.GET("/oauth/consent", { querySerializer: () => query })).data!,
  });
}

export function useDecideConsent() {
  return useMutation({
    mutationFn: async (body: ConsentDecision): Promise<string> => (await api.POST("/oauth/consent", { body })).data!.redirectTo,
  });
}

/** An app that may act for people here: a partner that trades its sign-in tokens, or one that registered itself. Its secret is there only in the answer that made it. */
export type ConnectedApp = Wire["Client"];
export type ConnectedAppInput = Wire["ClientInput"];
export type ConnectedAppChanges = Wire["ClientChanges"];
/** Anybody's token, as an administrator sees it, with whose it is. */
export type InstallationToken = Wire["SiteAPIToken"];

export const connectedAppsQueryKey = ["oauth", "clients"] as const;
export const installationTokensQueryKey = ["site", "tokens"] as const;

export function useConnectedApps() {
  return useQuery({
    queryKey: connectedAppsQueryKey,
    queryFn: async (): Promise<ConnectedApp[]> => (await api.GET("/oauth/clients")).data!.clients,
  });
}

export function useCreateConnectedApp() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (body: ConnectedAppInput): Promise<ConnectedApp> => (await api.POST("/oauth/clients", { body })).data!.client,
    onSettled: () => queryClient.invalidateQueries({ queryKey: connectedAppsQueryKey }),
  });
}

export function useUpdateConnectedApp() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async ({ id, changes }: { id: string; changes: ConnectedAppChanges }): Promise<ConnectedApp> =>
      (await api.PATCH("/oauth/clients/{clientID}", { params: { path: { clientID: id } }, body: changes })).data!.client,
    onSettled: () => queryClient.invalidateQueries({ queryKey: connectedAppsQueryKey }),
  });
}

export function useDeleteConnectedApp() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (id: string) => {
      await api.DELETE("/oauth/clients/{clientID}", { params: { path: { clientID: id } } });
    },
    onSettled: () =>
      Promise.all([
        queryClient.invalidateQueries({ queryKey: connectedAppsQueryKey }),
        queryClient.invalidateQueries({ queryKey: installationTokensQueryKey }),
      ]),
  });
}

/** Every token of the installation, people's own and the grants of connected apps. */
export function useInstallationTokens() {
  return useQuery({
    queryKey: installationTokensQueryKey,
    queryFn: async (): Promise<InstallationToken[]> => (await api.GET("/site/tokens")).data!.tokens,
  });
}

export function useRevokeInstallationToken() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (id: string) => {
      await api.DELETE("/site/tokens/{tokenID}", { params: { path: { tokenID: id } } });
    },
    onSettled: () =>
      Promise.all([
        queryClient.invalidateQueries({ queryKey: installationTokensQueryKey }),
        queryClient.invalidateQueries({ queryKey: connectedAppsQueryKey }),
      ]),
  });
}
