import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "./client";
import type { components } from "./schema";

type Wire = components["schemas"];

/** A personal access token; its secret is there only in the answer that made it. */
export type ApiToken = Wire["APIToken"];
export type NewApiToken = Wire["CreateTokenRequest"];

export const tokensQueryKey = ["tokens"] as const;

export function useTokens() {
  return useQuery({
    queryKey: tokensQueryKey,
    queryFn: async (): Promise<ApiToken[]> => (await api.GET("/tokens")).data!.tokens,
  });
}

export function useCreateToken() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (body: NewApiToken): Promise<ApiToken> => (await api.POST("/tokens", { body })).data!.token,
    onSettled: () => queryClient.invalidateQueries({ queryKey: tokensQueryKey }),
  });
}

export function useRevokeToken() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (id: string) => {
      await api.DELETE("/tokens/{tokenID}", { params: { path: { tokenID: id } } });
    },
    onSettled: () => queryClient.invalidateQueries({ queryKey: tokensQueryKey }),
  });
}
