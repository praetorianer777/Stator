import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { API_BASE, BRAND_STALE_MS } from "@/config";
import { api } from "./client";
import type { components } from "./schema";

/** What the organization's exports carry: a logo, or none, and a footer line in each language. */
export type Brand = components["schemas"]["Brand"];
export type BrandFooter = components["schemas"]["FooterInput"];

export const brandQueryKey = ["org", "brand"] as const;

export const brandQuery = {
  queryKey: brandQueryKey,
  queryFn: async (): Promise<Brand> => (await api.GET("/org/brand")).data!.brand,
  staleTime: BRAND_STALE_MS,
} as const;

export function useBrand() {
  return useQuery(brandQuery);
}

/** Where the logo is fetched from; the version changes with each upload, so a browser may keep each. */
export function brandLogoHref(brand: Brand): string | null {
  return brand.logo ? `${API_BASE}/org/brand/logo?v=${brand.logo.version}` : null;
}

function useBrandMutation<V>(run: (variables: V) => Promise<Brand>) {
  const queryClient = useQueryClient();
  return useMutation({ mutationFn: run, onSuccess: (saved) => queryClient.setQueryData(brandQueryKey, saved) });
}

export function useSetBrandFooter() {
  return useBrandMutation(async (body: BrandFooter) => (await api.PUT("/org/brand/footer", { body })).data!.brand);
}

// A multipart body in the generated types is an object of strings; the one
// that is sent is the form itself, which the client passes through untouched.
function fileForm(file: File): { file: string } {
  const form = new FormData();
  form.append("file", file, file.name);
  return form as unknown as { file: string };
}

export function useSetBrandLogo() {
  return useBrandMutation(async (file: File) => (await api.PUT("/org/brand/logo", { body: fileForm(file) })).data!.brand);
}

export function useRemoveBrandLogo() {
  return useBrandMutation(async (_: void) => (await api.DELETE("/org/brand/logo")).data!.brand);
}
