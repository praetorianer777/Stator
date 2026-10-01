import { useRef, useState } from "react";
import { useArmatureAccount, useArmatureProjects } from "@/api/armature";
import type { IssueSource } from "@/features/editor/armatureIssue";

/**
 * What the page editor turns into chips: addresses of the connected Armature,
 * and keys in projects the author may see there. The editor reads it as text
 * arrives, so it follows the answers without being built again.
 */
export function useIssueSource(pageId?: string): IssueSource {
  const account = useArmatureAccount();
  const asks = Boolean(account.data?.connected && account.data.status !== "rejected");
  const projects = useArmatureProjects(asks);
  const current = useRef<{ baseUrl: string | null; projects: ReadonlySet<string>; connected: boolean; pageId: string | null }>({
    baseUrl: null,
    projects: new Set(),
    connected: false,
    pageId: null,
  });
  current.current = {
    baseUrl: account.data?.configured ? (account.data.baseUrl ?? null) : null,
    projects: new Set(projects.data?.status === "ok" ? projects.data.projects.map((project) => project.key) : []),
    // A rejected token still offers the action, so the dialog can say why it fails.
    connected: Boolean(account.data?.configured && account.data.connected),
    pageId: pageId ?? null,
  };
  const [source] = useState<IssueSource>(() => ({
    baseUrl: () => current.current.baseUrl,
    knowsProject: (projectKey) => current.current.projects.has(projectKey),
    canCreate: () => current.current.connected && current.current.pageId !== null,
    pageId: () => current.current.pageId,
  }));
  return source;
}
