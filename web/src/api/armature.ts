import { useMutation, useQueries, useQuery, useQueryClient, type QueryClient } from "@tanstack/react-query";
import { api } from "./client";
import type { components } from "./schema";

type Wire = components["schemas"];

/** The organization's Armature instance, as its administrators see it; the webhook secret never comes back. */
export type ArmatureConnection = Wire["Connection"];
export type ArmatureConnectionInput = Wire["ConnectionInput"];
/** The caller's own link to Armature; the token never comes back. */
export type ArmatureAccount = Wire["Account"];
export type ArmatureStatus = ArmatureAccount["status"];

export const armatureConnectionQueryKey = ["armature", "connection"] as const;
export const armatureAccountQueryKey = ["armature", "account"] as const;

export function useArmatureConnection() {
  return useQuery({
    queryKey: armatureConnectionQueryKey,
    queryFn: async (): Promise<ArmatureConnection | null> => (await api.GET("/armature/connection")).data!.connection,
  });
}

// A new address forgets every member's token, so the caller's own account is
// asked again after any change to the connection.
export function useSaveArmatureConnection() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (body: ArmatureConnectionInput): Promise<ArmatureConnection> => (await api.PUT("/armature/connection", { body })).data!.connection,
    onSuccess: (connection) => {
      queryClient.setQueryData(armatureConnectionQueryKey, connection);
      void queryClient.invalidateQueries({ queryKey: armatureAccountQueryKey });
      forgetIssues(queryClient);
    },
  });
}

export function useRemoveArmatureConnection() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async () => {
      await api.DELETE("/armature/connection");
    },
    onSuccess: () => {
      queryClient.setQueryData(armatureConnectionQueryKey, null);
      void queryClient.invalidateQueries({ queryKey: armatureAccountQueryKey });
      forgetIssues(queryClient);
    },
  });
}

export function useArmatureAccount() {
  return useQuery({
    queryKey: armatureAccountQueryKey,
    queryFn: async (): Promise<ArmatureAccount> => (await api.GET("/armature/account")).data!.account,
  });
}

export function useConnectArmature() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (token: string): Promise<ArmatureAccount> => (await api.PUT("/armature/account/token", { body: { token } })).data!.account,
    onSuccess: (account) => {
      queryClient.setQueryData(armatureAccountQueryKey, account);
      forgetIssues(queryClient);
    },
  });
}

export function useCheckArmature() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (): Promise<ArmatureAccount> => (await api.POST("/armature/account/check")).data!.account,
    onSuccess: (account) => {
      queryClient.setQueryData(armatureAccountQueryKey, account);
      forgetIssues(queryClient);
    },
  });
}

export function useDisconnectArmature() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async () => {
      await api.DELETE("/armature/account/token");
    },
    onSettled: () => {
      void queryClient.invalidateQueries({ queryKey: armatureAccountQueryKey });
      forgetIssues(queryClient);
    },
  });
}

/** An Armature issue as a chip or a card draws it, as the viewer may see it. */
export type ArmatureIssue = Wire["Issue"];
export type ArmatureProject = Wire["Project"];

/** What a lookup found: the status, and each key's issue, null when the viewer may not see it. */
export interface ArmatureIssues {
  status: ArmatureStatus | undefined;
  issues: ReadonlyMap<string, ArmatureIssue | null>;
}

const issuesQueryKey = ["armature", "issues"] as const;
const issueQueryKey = ["armature", "issue"] as const;
const projectsQueryKey = ["armature", "projects"] as const;

// What a page shows of Armature depends on whose token asks, so a new or
// forgotten token asks again.
function forgetIssues(queryClient: QueryClient) {
  for (const queryKey of [issuesQueryKey, issueQueryKey, projectsQueryKey]) void queryClient.invalidateQueries({ queryKey });
}

/** One lookup per batch of keys; a status other than ok in any batch is the answer's. */
export function useArmatureIssues(batches: readonly (readonly string[])[], enabled: boolean): ArmatureIssues {
  return useQueries({
    queries: batches.map((keys) => ({
      queryKey: [...issuesQueryKey, ...keys],
      enabled,
      queryFn: async () => (await api.GET("/armature/issues", { params: { query: { key: [...keys] } } })).data!,
    })),
    combine: (results) => {
      const issues = new Map<string, ArmatureIssue | null>();
      let status: ArmatureStatus | undefined;
      for (const result of results) {
        if (!result.data) continue;
        if (result.data.status !== "ok") status = result.data.status;
        else status ??= "ok";
        for (const found of result.data.issues) issues.set(found.key, found.issue);
      }
      return { status, issues };
    },
  });
}

/** One issue, for a chip's card; the server answers it from the same cache as the lookup. */
export function useArmatureIssue(key: string, enabled: boolean) {
  return useQuery({
    queryKey: [...issueQueryKey, key],
    enabled,
    queryFn: async () => (await api.GET("/armature/issues/{issueKey}", { params: { path: { issueKey: key } } })).data!,
  });
}

/** The Armature projects the caller may see; a typed key becomes a chip only in one of them. */
export function useArmatureProjects(enabled: boolean) {
  return useQuery({
    queryKey: projectsQueryKey,
    enabled,
    queryFn: async () => (await api.GET("/armature/projects")).data!,
  });
}
