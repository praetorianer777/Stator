import { useInfiniteQuery, useMutation, useQueries, useQuery, useQueryClient, type QueryClient } from "@tanstack/react-query";
import { ARMATURE_LINKS_POLL_MS, ARMATURE_LIST_PAGE_SIZE } from "@/config";
import { ApiError, api } from "./client";
import type { components } from "./schema";
import { armatureThemeQueryKey, themesQueryKey } from "./themes";

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
const searchQueryKey = ["armature", "search"] as const;
const issueTypesQueryKey = ["armature", "issueTypes"] as const;

// What a page shows of Armature depends on whose token asks, so a new or
// forgotten token asks again.
function forgetIssues(queryClient: QueryClient) {
  for (const queryKey of [issuesQueryKey, issueQueryKey, projectsQueryKey, searchQueryKey, issueTypesQueryKey])
    void queryClient.invalidateQueries({ queryKey });
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

/** One page of the issues a query matches, as the caller may see them; a query Armature cannot read is an ApiError bad_query. */
export type ArmatureSearchPage = Awaited<ReturnType<typeof searchPage>>;

async function searchPage(query: string, limit: number, offset: number) {
  return (await api.GET("/armature/search", { params: { query: { q: query, limit, offset } } })).data!;
}

/** The rows of an issue list, a page at a time up to its limit. */
export function useArmatureSearch(query: string, limit: number, enabled: boolean) {
  return useInfiniteQuery({
    queryKey: [...searchQueryKey, query, limit],
    enabled: enabled && query.trim() !== "",
    initialPageParam: 0,
    queryFn: ({ pageParam }) => searchPage(query, Math.min(ARMATURE_LIST_PAGE_SIZE, limit - pageParam), pageParam),
    getNextPageParam: (last, pages) => {
      if (last.status !== "ok") return undefined;
      const loaded = pages.reduce((n, page) => n + page.issues.length, 0);
      return last.issues.length > 0 && loaded < Math.min(last.total, limit) ? loaded : undefined;
    },
  });
}

/** A count of an NQL query's issues for a chart block, as the caller may see them. */
export type ArmatureChartAnswer = Awaited<ReturnType<typeof chartOf>>;

async function chartOf(settings: { project: string; query: string; chart: "pie" | "createdResolved"; groupBy: string; days: number }) {
  return (
    await api.GET("/armature/chart", {
      params: { query: { project: settings.project, q: settings.query, kind: settings.chart, groupBy: settings.groupBy as never, days: settings.days } },
    })
  ).data!;
}

/** A chart block's counts; kept with the searches, so an issue's change asks again. */
export function useArmatureChart(settings: Parameters<typeof chartOf>[0], enabled: boolean) {
  return useQuery({
    queryKey: [...searchQueryKey, "chart", settings.project, settings.query, settings.chart, settings.groupBy, settings.days],
    enabled: enabled && settings.query.trim() !== "",
    queryFn: () => chartOf(settings),
  });
}

/** The issue types a new issue may take, sub-tasks left out. */
export function useArmatureIssueTypes(enabled: boolean) {
  return useQuery({
    queryKey: issueTypesQueryKey,
    enabled,
    queryFn: async () => (await api.GET("/armature/issue-types")).data!,
  });
}

export type ArmatureCreateInput = Wire["CreateIssuesInput"];
/** The issues one create made, in order, and the item Armature refused, after which none was tried. */
export interface ArmatureCreated {
  issues: ArmatureIssue[];
  failed: Wire["CreateFailure"] | null;
}

// Never retried: a create that timed out may have reached Armature.
export function useCreateArmatureIssues() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (body: ArmatureCreateInput): Promise<ArmatureCreated> => (await api.POST("/armature/issues", { body })).data!,
    onError: (error) => {
      if (error instanceof ApiError && error.code === "armature_rejected") void queryClient.invalidateQueries({ queryKey: armatureAccountQueryKey });
    },
  });
}

/** Whether the caller follows their Armature theme, whether Armature answered, and why it cannot be used. */
export type ArmatureThemeFollow = Wire["ThemeFollow"];

export { armatureThemeQueryKey };

// Following changes the theme the page shows, which the loader reads from
// the active theme; the list of themes says which one is in use.
function themeChanged(queryClient: QueryClient) {
  void queryClient.invalidateQueries({ queryKey: themesQueryKey });
}

/** Asks Armature now, so a theme changed there shows when the settings open. */
export function useArmatureThemeFollow(enabled: boolean) {
  const queryClient = useQueryClient();
  return useQuery({
    queryKey: armatureThemeQueryKey,
    enabled,
    queryFn: async (): Promise<ArmatureThemeFollow> => {
      const follow = (await api.GET("/armature/theme")).data!.follow;
      if (follow.following) themeChanged(queryClient);
      return follow;
    },
  });
}

export function useFollowArmatureTheme() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (): Promise<ArmatureThemeFollow> => (await api.PUT("/armature/theme")).data!.follow,
    onSuccess: (follow) => {
      queryClient.setQueryData(armatureThemeQueryKey, follow);
      themeChanged(queryClient);
    },
  });
}

export function useUnfollowArmatureTheme() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async () => {
      await api.DELETE("/armature/theme");
    },
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: armatureThemeQueryKey });
      themeChanged(queryClient);
    },
  });
}

/** Whether Armature can read a query, and how many issues it matches for the caller. */
export function useArmatureQueryCheck(query: string, enabled: boolean) {
  return useQuery({
    queryKey: [...searchQueryKey, "check", query],
    enabled,
    queryFn: () => searchPage(query, 1, 0),
  });
}

/** Whether the page's remote link reached one issue it names in Armature. */
export type ArmatureLink = Wire["Link"];

/** The links of a published version of a page, asked again while any waits for the worker. */
export function useArmatureLinks(pageId: string, version: number, enabled: boolean) {
  return useQuery({
    queryKey: ["armature", "links", pageId, version],
    enabled,
    queryFn: async (): Promise<ArmatureLink[]> => (await api.GET("/pages/{pageID}/armature-links", { params: { path: { pageID: pageId } } })).data!.links,
    refetchInterval: (query) => (query.state.data?.some((link) => link.state === "pending") ? ARMATURE_LINKS_POLL_MS : false),
  });
}
