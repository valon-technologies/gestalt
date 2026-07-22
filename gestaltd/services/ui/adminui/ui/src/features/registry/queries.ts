import { queryOptions } from "@tanstack/react-query";
import { adminFetch } from "@/lib/api";
import type { MaterializationsResponse, RegistryAppDetail, RegistryAppSummary } from "@/features/registry/types";

export const registryQueries = {
  all: () => ["registry"] as const,
  apps: () =>
    queryOptions({
      queryKey: [...registryQueries.all(), "apps"] as const,
      queryFn: () => adminFetch<RegistryAppSummary[]>("/registry-apps"),
    }),
  app: (app: string) =>
    queryOptions({
      queryKey: [...registryQueries.all(), "app", app] as const,
      queryFn: () => adminFetch<RegistryAppDetail>(`/registry-apps/${encodeURIComponent(app)}`),
    }),
  materializations: (app: string) =>
    queryOptions({
      queryKey: [...registryQueries.all(), "materializations", app] as const,
      queryFn: () => adminFetch<MaterializationsResponse>(`/app-rollouts/${encodeURIComponent(app)}/materializations`),
    }),
};
