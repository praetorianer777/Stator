import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { language } from "@/i18n";
import { api } from "./client";
import { spaceQueryKey, spacesQueryKey, type Space } from "./spaces";

export const exampleSpaceQueryKey = ["example-space"] as const;

/** The organization's example space, null while there is none; asked only by administrators. */
export function useExampleSpace(enabled: boolean) {
  return useQuery({
    queryKey: exampleSpaceQueryKey,
    queryFn: async (): Promise<Space | null> => (await api.GET("/example-space")).data!.space ?? null,
    enabled,
  });
}

/** What making the example answered: the space, and whether this click made it. */
export interface ExampleSpaceResult {
  space: Space;
  created: boolean;
}

/** Makes the example space in the language the interface speaks, or finds the one there is. */
export function useCreateExampleSpace() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (): Promise<ExampleSpaceResult> => (await api.POST("/example-space", { body: { language: language() } })).data!,
    onSuccess: ({ space }) => {
      queryClient.setQueryData(exampleSpaceQueryKey, space);
      queryClient.setQueryData(spaceQueryKey(space.key), space);
      return queryClient.invalidateQueries({ queryKey: spacesQueryKey });
    },
  });
}
