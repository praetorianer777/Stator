import { keepPreviousData, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { PICKER_LIMIT } from "@/config";
import { ApiError, api } from "./client";
import { pagesQueryKey } from "./pages";
import { spaceAnonymousAccessQueryKey } from "./public";
import type { components } from "./schema";
import { spacesQueryKey } from "./spaces";
import { treeQueryKey } from "./tree";

type Wire = components["schemas"];

/** What the caller may do across the organization. */
export type GlobalCan = Wire["GlobalCan"];
export type GlobalGrant = Wire["GlobalGrant"];
export type GlobalPermission = GlobalGrant["permission"];
/** Somebody a grant or a restriction names, with the name to show. */
export type Subject = Wire["Subject"];
export type SubjectRef = Wire["SubjectRef"];
export type SubjectType = Subject["type"];
export type SpaceGrant = Wire["SpaceGrant"];
export type SpacePermission = SpaceGrant["permissions"][number];
export type Restrictions = Wire["Restrictions"];
export type InheritedRestriction = Wire["InheritedRestriction"];
export type Person = Wire["Person"];
/** What one person may do to one page, and the steps behind each right, as the database answers it. */
export type AccessReport = Wire["AccessReport"];
export type AccessRight = AccessReport["rights"][number];
export type AccessStep = Wire["AccessStep"];
export type Group = Wire["Group"];

/** The organization's permissions in the order the settings list them. */
export const GLOBAL_PERMISSIONS: GlobalPermission[] = ["use", "createSpace", "administer"];
/** A space's permissions in the order its grid shows them, weakest first. */
export const SPACE_PERMISSIONS: SpacePermission[] = ["view", "addPages", "addComments", "delete", "administer"];

export const accessQueryKey = ["access", "me"] as const;
export const orgPermissionsQueryKey = ["org", "permissions"] as const;

export function spacePermissionsQueryKey(spaceKey: string) {
  return ["space-permissions", spaceKey.toUpperCase()] as const;
}

export function restrictionsQueryKey(pageId: string) {
  return ["restrictions", pageId] as const;
}

/** Whether an error is the organization refusing the caller altogether, which every route but sign-in answers. */
export function isNoAccess(error: unknown): boolean {
  return error instanceof ApiError && error.status === 403 && error.code === "no_access";
}

/** The request form of a subject: everyone carries no id. */
export function subjectRef(subject: Subject): SubjectRef {
  return subject.type === "everyone" || !subject.id ? { type: subject.type } : { type: subject.type, id: subject.id };
}

/** One string per subject, for keys and for telling two apart. */
export function subjectKey(subject: Pick<Subject, "type" | "id">): string {
  return subject.type === "everyone" ? "everyone" : `${subject.type}:${subject.id ?? ""}`;
}

export const accessQuery = {
  queryKey: accessQueryKey,
  queryFn: async (): Promise<GlobalCan> => (await api.GET("/access/me")).data!.can,
} as const;

/** What the caller may do across the organization; undefined while it loads or when the API cannot say. */
export function useAccess() {
  return useQuery(accessQuery);
}

export function useOrgPermissions() {
  return useQuery({
    queryKey: orgPermissionsQueryKey,
    queryFn: async (): Promise<GlobalGrant[]> => (await api.GET("/org/permissions")).data!.permissions,
  });
}

export function useSetOrgPermission() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async ({ permission, subjects }: { permission: GlobalPermission; subjects: SubjectRef[] }): Promise<GlobalGrant> =>
      (await api.PUT("/org/permissions/{permission}", { params: { path: { permission } }, body: { subjects } })).data!.permission,
    onSuccess: (saved) => {
      queryClient.setQueryData<GlobalGrant[]>(orgPermissionsQueryKey, (before) => before?.map((each) => (each.permission === saved.permission ? saved : each)));
      return queryClient.invalidateQueries({ queryKey: accessQueryKey });
    },
  });
}

export function useSpacePermissions(spaceKey: string, enabled = true) {
  return useQuery({
    queryKey: spacePermissionsQueryKey(spaceKey),
    queryFn: async (): Promise<SpaceGrant[]> => (await api.GET("/spaces/{spaceKey}/permissions", { params: { path: { spaceKey } } })).data!.grants,
    enabled,
  });
}

export function useSetSpacePermissions(spaceKey: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (grants: SpaceGrant[]): Promise<SpaceGrant[]> =>
      (
        await api.PUT("/spaces/{spaceKey}/permissions", {
          params: { path: { spaceKey } },
          body: { grants: grants.map((grant) => ({ subject: subjectRef(grant.subject), permissions: grant.permissions })) },
        })
      ).data!.grants,
    // What the caller may do in the space, and so what the space, its tree and its pages show them, follows from the table.
    onSuccess: (saved) => {
      queryClient.setQueryData(spacePermissionsQueryKey(spaceKey), saved);
      return Promise.all([
        queryClient.invalidateQueries({ queryKey: spacesQueryKey }),
        queryClient.invalidateQueries({ queryKey: treeQueryKey }),
        queryClient.invalidateQueries({ queryKey: pagesQueryKey }),
      ]);
    },
  });
}

/** What copying another space's permissions onto one would change, with the fingerprint that applies exactly that. */
export type PermissionCopyPreview = Wire["PermissionCopyPreview"];
export type PermissionCopyMode = PermissionCopyPreview["mode"];
export type PermissionCopyChange = Wire["CopyChange"];
export type CopySubject = Wire["CopySubject"];

export function permissionCopyQueryKey(spaceKey: string, from: string, mode: PermissionCopyMode) {
  return [...spacePermissionsQueryKey(spaceKey), "copy", from.toUpperCase(), mode] as const;
}

/** The preview of a copy from another space, asked once a source is chosen; never kept, since applying needs it current. */
export function usePermissionCopyPreview(spaceKey: string, from: string, mode: PermissionCopyMode) {
  return useQuery({
    queryKey: permissionCopyQueryKey(spaceKey, from, mode),
    queryFn: async (): Promise<PermissionCopyPreview> =>
      (await api.GET("/spaces/{spaceKey}/permissions/copy", { params: { path: { spaceKey }, query: { from, mode } } })).data!.preview,
    enabled: Boolean(from),
    gcTime: 0,
    retry: false,
  });
}

export function useCopyPermissions(spaceKey: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (preview: PermissionCopyPreview): Promise<SpaceGrant[]> =>
      (
        await api.POST("/spaces/{spaceKey}/permissions/copy", {
          params: { path: { spaceKey } },
          body: { from: preview.source.key, mode: preview.mode, fingerprint: preview.fingerprint },
        })
      ).data!.grants,
    // A copy may open the space to anybody or close it, besides what the grid shows.
    onSuccess: (saved) => {
      queryClient.setQueryData(spacePermissionsQueryKey(spaceKey), saved);
      return Promise.all([
        queryClient.invalidateQueries({ queryKey: spaceAnonymousAccessQueryKey(spaceKey) }),
        queryClient.invalidateQueries({ queryKey: spacesQueryKey }),
        queryClient.invalidateQueries({ queryKey: treeQueryKey }),
        queryClient.invalidateQueries({ queryKey: pagesQueryKey }),
      ]);
    },
  });
}

export function usePageRestrictions(pageId: string) {
  return useQuery({
    queryKey: restrictionsQueryKey(pageId),
    queryFn: async (): Promise<Restrictions> => (await api.GET("/pages/{pageID}/restrictions", { params: { path: { pageID: pageId } } })).data!.restrictions,
  });
}

export function useSetPageRestrictions(pageId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (lists: { view: Subject[]; edit: Subject[] }): Promise<Restrictions> =>
      (
        await api.PUT("/pages/{pageID}/restrictions", {
          params: { path: { pageID: pageId } },
          body: { view: lists.view.map(subjectRef), edit: lists.edit.map(subjectRef) },
        })
      ).data!.restrictions,
    // The pages below inherit the lists, so every page and tree level read may have changed.
    onSuccess: (saved) => {
      queryClient.setQueryData(restrictionsQueryKey(pageId), saved);
      return Promise.all([
        queryClient.invalidateQueries({ queryKey: ["restrictions"] }),
        queryClient.invalidateQueries({ queryKey: treeQueryKey }),
        queryClient.invalidateQueries({ queryKey: pagesQueryKey }),
      ]);
    },
  });
}

export function inspectAccessQueryKey(pageId: string, userId: string) {
  return ["page-access", pageId, userId] as const;
}

/** Why a person may or may not view, edit, trash and comment on a page; for administrators of its space. */
export function useInspectAccess(pageId: string, userId: string | undefined) {
  return useQuery({
    queryKey: inspectAccessQueryKey(pageId, userId ?? ""),
    queryFn: async (): Promise<AccessReport> =>
      (await api.GET("/pages/{pageID}/access/{userID}", { params: { path: { pageID: pageId, userID: userId ?? "" } } })).data!.access,
    enabled: Boolean(userId),
  });
}

/** What a picker finds for a text; a share's picker also says whether each may view its page. */
export interface SubjectSearchResult {
  people: Array<Person & { canView?: boolean }>;
  groups: Array<Group & { viewers?: number }>;
  error: Error | null;
  /** The lists answer q itself, not a previous text kept up while q is asked. */
  current: boolean;
}

/** A hook that finds people and groups for a picker. */
export type SubjectSearch = (q: string, enabled: boolean, withGroups?: boolean) => SubjectSearchResult;

/** People and groups whose name, or a person's email, starts with q, for a picker; asked only while it is open. */
export function useSubjectSearch(q: string, enabled: boolean, withGroups = true): SubjectSearchResult {
  const query = { q: q || undefined, limit: PICKER_LIMIT };
  const people = useQuery({
    queryKey: ["people", query],
    queryFn: async (): Promise<Person[]> => (await api.GET("/people", { params: { query } })).data!.people,
    enabled,
    placeholderData: keepPreviousData,
  });
  const groups = useQuery({
    queryKey: ["groups", query],
    queryFn: async (): Promise<Group[]> => (await api.GET("/groups", { params: { query } })).data!.groups,
    enabled: enabled && withGroups,
    placeholderData: keepPreviousData,
  });
  return {
    people: people.data ?? [],
    groups: withGroups ? (groups.data ?? []) : [],
    error: people.error ?? groups.error,
    /** The lists answer q itself, not a previous text kept up while q is asked. */
    current: people.data !== undefined && !people.isPlaceholderData && (!withGroups || (groups.data !== undefined && !groups.isPlaceholderData)),
  };
}
