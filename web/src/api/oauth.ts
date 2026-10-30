import { useQuery } from "@tanstack/react-query";
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
