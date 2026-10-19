import { useRef, useState } from "react";
import { useExciterAccount } from "@/api/exciter";
import type { RequirementSource } from "@/features/editor/exciterRequirement";

/**
 * Whether the page editor offers Exciter: the slash menu's entry while Stator
 * is connected to an Exciter, and typed keys as chips once the author connected
 * their own account. The editor reads it as text arrives, without being built again.
 */
export function useRequirementSource(): RequirementSource {
  const account = useExciterAccount();
  const current = useRef({ baseUrl: null as string | null, connected: false });
  current.current = {
    baseUrl: account.data?.configured ? (account.data.baseUrl ?? null) : null,
    connected: Boolean(account.data?.configured && account.data.connected && account.data.status !== "rejected"),
  };
  const [source] = useState<RequirementSource>(() => ({
    baseUrl: () => current.current.baseUrl,
    connected: () => current.current.connected,
  }));
  return source;
}
