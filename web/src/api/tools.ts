import { useQuery } from "@tanstack/react-query";
import { api } from "./client";
import type { components } from "./schema";

/** Another tool of the installation Stator is connected to, at the address people open it at. */
export type ConnectedTool = components["schemas"]["ConnectedTool"];
export type ToolProduct = ConnectedTool["product"];

export const connectedToolsQueryKey = ["connected-tools"] as const;

export function useConnectedTools(enabled = true) {
  return useQuery({
    queryKey: connectedToolsQueryKey,
    enabled,
    queryFn: async (): Promise<ConnectedTool[]> => (await api.GET("/connected-tools")).data!.tools,
  });
}
