import { useQuery } from "@tanstack/react-query";
import { api } from "./client";
import type { components } from "./schema";
import { labelsQueryKey } from "./labels";

/** A register built from the properties of the pages carrying some labels. */
export type PropertiesReport = components["schemas"]["PropertiesReport"];

/** A properties report's rows, as the reader may read them; kept with the labels, so a label's change asks again. */
export function usePropertiesReport(settings: { labels: string[]; space: string | null; columns: string[] }) {
  return useQuery({
    queryKey: [...labelsQueryKey, "report", settings.labels, settings.space, settings.columns],
    enabled: settings.labels.length > 0,
    queryFn: async () =>
      (
        await api.GET("/properties-report", {
          params: { query: { label: settings.labels, space: settings.space ?? undefined, column: settings.columns.length > 0 ? settings.columns : undefined } },
        })
      ).data!,
  });
}
