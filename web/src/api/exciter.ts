import { useInfiniteQuery, useMutation, useQueries, useQuery, useQueryClient, type QueryClient } from "@tanstack/react-query";
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

/** A requirement as a requirement block draws it: the chip's fields with its statement and rationale, documents in Exciter's schema. */
export type ExciterRequirementDetail = Wire["RequirementDetail"];
/** A requirement the picker offers, with the project it is in. */
export type ExciterHit = Wire["ExciterHit"];
/** A requirement as a list block shows it. */
export type ExciterRow = Wire["ExciterRow"];
export type ExciterProject = Wire["ExciterProject"];
/** What may come next at the caret of a list block's query. */
export type ExciterSuggestions = Wire["ExciterSuggestions"];

const listsQueryKey = [...requirementsQueryKey, "lists"] as const;

/** One requirement with its statement and rationale, for a requirement block. */
export function useExciterRequirement(key: string, enabled: boolean) {
  return useQuery({
    queryKey: [...requirementsQueryKey, "detail", key],
    enabled: enabled && key !== "",
    queryFn: async () => (await api.GET("/exciter/requirements/{reqKey}", { params: { path: { reqKey: key } } })).data!,
  });
}

/** Requirements by the start of their key or title, for the picker; a newer word aborts the request still running. */
export function useExciterSearch(words: string, enabled: boolean) {
  return useQuery({
    queryKey: [...requirementsQueryKey, "search", words],
    enabled: enabled && words.trim() !== "",
    queryFn: async ({ signal }) => (await api.GET("/exciter/search", { params: { query: { q: words } }, signal })).data!,
  });
}

/** The Exciter projects the caller may view, for a list block's editor. */
export function useExciterProjects(enabled: boolean) {
  return useQuery({
    queryKey: [...requirementsQueryKey, "projects"],
    enabled,
    queryFn: async () => (await api.GET("/exciter/projects")).data!,
  });
}

/** One window of a list block's query; a query Exciter does not read is an ApiError bad_query with its position. */
export type ExciterListPage = Awaited<ReturnType<typeof listPage>>;

async function listPage(project: string, query: string, limit: number, offset: number) {
  return (
    await api.GET("/exciter/projects/{projectKey}/requirements", {
      params: { path: { projectKey: project }, query: { query, limit, offset } },
    })
  ).data!;
}

/** The rows of a requirement list, a window at a time up to its limit. */
export function useExciterList(project: string, query: string, limit: number, pageSize: number, enabled: boolean) {
  return useInfiniteQuery({
    queryKey: [...listsQueryKey, project, query, limit],
    enabled: enabled && project !== "",
    initialPageParam: 0,
    queryFn: ({ pageParam }) => listPage(project, query, Math.min(pageSize, limit - pageParam), pageParam),
    getNextPageParam: (last, pages) => {
      if (last.status !== "ok") return undefined;
      const loaded = pages.reduce((n, page) => n + page.requirements.length, 0);
      return last.requirements.length > 0 && loaded < Math.min(last.total, limit) ? loaded : undefined;
    },
  });
}

/** Asks Exciter whether a query reads, and how many requirements it finds, as the list's editor types it. */
export function useExciterQueryCheck(project: string, query: string, enabled: boolean) {
  return useQuery({
    queryKey: [...listsQueryKey, "check", project, query],
    enabled: enabled && project !== "",
    queryFn: () => listPage(project, query, 1, 0),
  });
}

/** What may come next at the caret at, counted in characters, of a list block's query. */
export function useExciterSuggest(project: string, q: string, at: number, enabled: boolean) {
  return useQuery({
    queryKey: [...requirementsQueryKey, "suggest", project, q, at],
    enabled: enabled && project !== "",
    queryFn: async ({ signal }) =>
      (await api.GET("/exciter/projects/{projectKey}/requirements/suggest", { params: { path: { projectKey: project }, query: { q, at } }, signal })).data!
        .suggestions,
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
