import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { EXAMPLE_SPACE_POLL_MS } from "@/config";
import { language } from "@/i18n";
import { api } from "./client";
import type { components } from "./schema";
import { spaceQueryKey, spacesQueryKey } from "./spaces";

type Wire = components["schemas"];

/** One making of the example space, which the worker does while the page follows it. */
export type ExampleSpaceJob = Wire["ExampleSpaceJob"];
/** The example space once it is made, the latest making or renewal of it, and which version of its content it has. */
export type ExampleSpaceState = Wire["ExampleSpaceResponse"];
/** Which version of the content the example has, which is current, and what the newer ones bring. */
export type ExampleSpaceEdition = Wire["ExampleSpaceEdition"];
/** What a renewal added, and what it left to people. */
export type ExampleSpaceReport = Wire["ExampleSpaceReport"];

export const exampleSpaceQueryKey = ["example-space"] as const;

/** Whether the job is still to finish, so the page keeps asking. */
export function jobOpen(job: ExampleSpaceJob | null | undefined): job is ExampleSpaceJob {
  return job?.state === "queued" || job?.state === "running";
}

/** The example space and its latest making, asked again while it is being made; asked only by administrators. */
export function useExampleSpace(enabled: boolean, follow = true) {
  return useQuery({
    queryKey: exampleSpaceQueryKey,
    queryFn: async (): Promise<ExampleSpaceState> => (await api.GET("/example-space", { params: { query: { language: language() } } })).data!,
    enabled,
    refetchInterval: follow ? (query) => (jobOpen(query.state.data?.job) ? EXAMPLE_SPACE_POLL_MS : false) : false,
  });
}

/** Asks for the example space in the language the interface speaks: the job making it, or the one there is. */
export function useCreateExampleSpace() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (): Promise<ExampleSpaceState> => (await api.POST("/example-space", { body: { language: language() } })).data!,
    onSuccess: (answer) => {
      queryClient.setQueryData(exampleSpaceQueryKey, answer);
      if (answer.space) queryClient.setQueryData(spaceQueryKey(answer.space.key), answer.space);
    },
  });
}

/** Whether a newer version of the content is there to renew the example to. */
export function renewable(edition: ExampleSpaceEdition | null | undefined): edition is ExampleSpaceEdition {
  return !!edition && edition.version < edition.current;
}

/** Asks the worker to renew the example, in the language given or the one its pages are in. */
export function useRenewExampleSpace() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (lang?: string): Promise<ExampleSpaceState> => (await api.POST("/example-space/renew", { body: lang ? { language: lang } : {} })).data!,
    onSuccess: (answer) => {
      queryClient.setQueryData<ExampleSpaceState>(exampleSpaceQueryKey, (was) => ({ ...answer, space: was?.space ?? null, edition: was?.edition ?? null }));
    },
  });
}

/** What follows a made example: the spaces overview lists it. */
export function exampleSpaceMade(queryClient: ReturnType<typeof useQueryClient>) {
  return queryClient.invalidateQueries({ queryKey: spacesQueryKey });
}
