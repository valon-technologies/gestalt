import { Badge } from "@/components/ui/badge";
import type { FleetState } from "@/features/registry/types";

type BadgeVariant = "success" | "warning" | "destructive" | "muted";

export function fleetStateVariant(state: FleetState["state"]): BadgeVariant {
  switch (state) {
    case "healthy":
      return "success";
    case "converging":
      return "warning";
    case "degraded":
      return "destructive";
    default:
      return "muted";
  }
}

export function fleetCapacityLabel(fleet: FleetState) {
  if (fleet.minimumHealthyInstances <= 0) {
    return `${fleet.liveInstances} live · expected count unavailable`;
  }
  return `${fleet.liveInstances}/${fleet.minimumHealthyInstances} live`;
}

export function fleetDiagnostic(fleet: FleetState) {
  if (fleet.minimumHealthyInstances <= 0 || !fleet.sourceVersion || !fleet.desiredVersion) {
    return "Fleet basis unavailable";
  }
  if (fleet.liveInstances < fleet.minimumHealthyInstances) {
    return `Insufficient capacity: ${fleet.liveInstances}/${fleet.minimumHealthyInstances} live`;
  }
  if (fleet.mismatched > 0 || fleet.errors > 0) {
    return `${fleet.mismatched} version mismatch${fleet.mismatched === 1 ? "" : "es"} · ${fleet.errors} error${fleet.errors === 1 ? "" : "s"}`;
  }
  return `${fleet.runningDesiredVersion}/${fleet.liveInstances} running desired version`;
}

export function heartbeatAgeLabel(seconds: number, fresh: boolean) {
  const age = Math.max(0, Math.floor(seconds));
  if (!fresh) return `stale · ${age}s ago`;
  return `${age}s ago`;
}

export function replicaSourceLabel(currentSource: boolean) {
  return currentSource ? "current source" : "superseded source";
}

export function fleetRolloutSeparation(fleet: FleetState, rolloutState?: string) {
  if (fleet.state === "healthy" && rolloutState === "failed") {
    return "Current fleet is healthy; the last rollout remains failed.";
  }
  return "";
}

export function FleetStateBadge({ fleet }: { fleet: FleetState }) {
  return <Badge variant={fleetStateVariant(fleet.state)}>fleet {fleet.state}</Badge>;
}
