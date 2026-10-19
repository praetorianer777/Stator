import { useMutation, useQueries, useQuery, useQueryClient, type QueryClient } from "@tanstack/react-query";
import { api } from "./client";
import type { components } from "./schema";

type Wire = components["schemas"];

/** The Exciter installation Stator is connected to, as its administrators see it; the webhook secret never comes back. */
export type ExciterConnection = Wire["ExciterConnection"];
export type ExciterConnectionInput = Wire["ExciterConnectionInput"];
export type ExciterConnectionCheck = Wire["ExciterConnectionCheck"];
/** The caller's own link to Exciter; the token never comes back. */
export type ExciterAccount = Wire["ExciterAccount"];
export type ExciterStatus = ExciterAccount["status"];
/** A part of Stator that needs Exciter to offer a capability. */
export type ExciterFeature = Wire["ExciterMissingFeature"]["feature"];
export type ExciterPartner = Wire["ExciterPartner"];
/** An Exciter requirement as a chip draws it, as the viewer may see it. */
export type ExciterRequirement = Wire["Requirement"];

/** Whether the connected Exciter lacks what feature needs, as the account says; pages then say so instead of asking. */
export function exciterLacks(account: ExciterAccount | undefined, feature: ExciterFeature): boolean {
  return Boolean(account?.missing?.some((m) => m.feature === feature));
}

export const exciterConnectionQueryKey = ["exciter", "connection"] as const;
export const exciterAccountQueryKey = ["exciter", "account"] as const;
const requirementsQueryKey = ["exciter", "requirements"] as const;

// What a page shows of Exciter depends on whose token asks, so a new or
// forgotten token asks again.
function forgetRequirements(queryClient: QueryClient) {
  void queryClient.invalidateQueries({ queryKey: requirementsQueryKey });
}

export function useExciterConnection() {
  return useQuery({
    queryKey: exciterConnectionQueryKey,
    queryFn: async (): Promise<ExciterConnection | null> => (await api.GET("/exciter/connection")).data!.connection,
  });
}

// A new address forgets every member's token, so the caller's own account is
// asked again after any change to the connection.
export function useSaveExciterConnection() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (body: ExciterConnectionInput): Promise<ExciterConnection> => (await api.PUT("/exciter/connection", { body })).data!.connection,
    onSuccess: (connection) => {
      queryClient.setQueryData(exciterConnectionQueryKey, connection);
      void queryClient.invalidateQueries({ queryKey: exciterAccountQueryKey });
      forgetRequirements(queryClient);
    },
  });
}

/** Asks an address what answers there, before it is saved; a refusal is an ApiError with a sentence on baseUrl. */
export function useCheckExciterAddress() {
  return useMutation({
    mutationFn: async (baseUrl: string): Promise<ExciterConnectionCheck> => (await api.POST("/exciter/connection/check", { body: { baseUrl } })).data!.check,
  });
}

export function useRemoveExciterConnection() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async () => {
      await api.DELETE("/exciter/connection");
    },
    onSuccess: () => {
      queryClient.setQueryData(exciterConnectionQueryKey, null);
      void queryClient.invalidateQueries({ queryKey: exciterAccountQueryKey });
      forgetRequirements(queryClient);
    },
  });
}

export function useExciterAccount() {
  return useQuery({
    queryKey: exciterAccountQueryKey,
    queryFn: async (): Promise<ExciterAccount> => (await api.GET("/exciter/account")).data!.account,
  });
}

export function useConnectExciter() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (token: string): Promise<ExciterAccount> => (await api.PUT("/exciter/account/token", { body: { token } })).data!.account,
    onSuccess: (account) => {
      queryClient.setQueryData(exciterAccountQueryKey, account);
      forgetRequirements(queryClient);
    },
  });
}

export function useCheckExciter() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (): Promise<ExciterAccount> => (await api.POST("/exciter/account/check")).data!.account,
    onSuccess: (account) => {
      queryClient.setQueryData(exciterAccountQueryKey, account);
      forgetRequirements(queryClient);
    },
  });
}

export function useDisconnectExciter() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async () => {
      await api.DELETE("/exciter/account/token");
    },
    onSettled: () => {
      void queryClient.invalidateQueries({ queryKey: exciterAccountQueryKey });
      forgetRequirements(queryClient);
    },
  });
}

/** What a lookup found: the status, and each key's requirement, null when the viewer may not see it. */
export interface ExciterRequirements {
  status: ExciterStatus | undefined;
  requirements: ReadonlyMap<string, ExciterRequirement | null>;
}

/** One lookup per batch of keys; a status other than ok in any batch is the answer's. */
export function useExciterRequirements(batches: readonly (readonly string[])[], enabled: boolean): ExciterRequirements {
  return useQueries({
    queries: batches.map((keys) => ({
      queryKey: [...requirementsQueryKey, ...keys],
      enabled,
      queryFn: async () => (await api.GET("/exciter/requirements", { params: { query: { key: [...keys] } } })).data!,
    })),
    combine: (results) => {
      const requirements = new Map<string, ExciterRequirement | null>();
      let status: ExciterStatus | undefined;
      for (const result of results) {
        if (!result.data) continue;
        if (result.data.status !== "ok") status = result.data.status;
        else status ??= "ok";
        for (const found of result.data.requirements) requirements.set(found.key, found.requirement);
      }
      return { status, requirements };
    },
  });
}
