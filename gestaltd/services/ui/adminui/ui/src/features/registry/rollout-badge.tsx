import { Badge } from "@/components/ui/badge";
import type { RegistryAppSummary } from "@/features/registry/types";

export function rolloutBadgeLabel(app: RegistryAppSummary) {
  return app.rollout?.state || (app.desiredVersion ? "not started" : "not installed");
}

export function rolloutBadgeVariant(state: string): "success" | "warning" | "destructive" | "muted" {
  switch (state) {
    case "complete":
      return "success";
    case "failed":
      return "destructive";
    case "enrolling":
    case "restarting":
      return "warning";
    default:
      return "muted";
  }
}

export function RolloutBadge({ app }: { app: RegistryAppSummary }) {
  const label = rolloutBadgeLabel(app);
  return <Badge variant={rolloutBadgeVariant(label)}>{label}</Badge>;
}
