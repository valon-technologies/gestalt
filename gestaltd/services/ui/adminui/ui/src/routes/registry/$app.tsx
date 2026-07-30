import { useEffect, type ReactNode } from "react";
import { useQuery } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import { Code } from "@/components/ui/code";
import {
  SectionHeader,
  SectionHeaderActions,
  SectionHeaderContent,
  SectionHeaderDescription,
  SectionHeaderTitle,
} from "@/components/ui/section-header";
import { Skeleton } from "@/components/ui/skeleton";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import {
  FleetStateBadge,
  fleetCapacityLabel,
  fleetDiagnostic,
  fleetRolloutSeparation,
  heartbeatAgeLabel,
  replicaSourceLabel,
} from "@/features/registry/fleet-state";
import { registryQueries } from "@/features/registry/queries";
import { RolloutBadge } from "@/features/registry/rollout-badge";
import type { FleetReplica } from "@/features/registry/types";

export const Route = createFileRoute("/registry/$app")({
  component: RegistryAppDetailPage,
});

function formatTime(value?: string | null) {
  return value ? new Date(value).toLocaleString() : "—";
}

function SummaryField({
  label,
  children,
}: {
  label: string;
  children: ReactNode;
}) {
  return (
    <div className="min-w-0">
      <dt className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">{label}</dt>
      <dd className="mt-1 min-w-0">{children}</dd>
    </div>
  );
}

function ReplicaTable({ replicas }: { replicas: FleetReplica[] }) {
  if (replicas.length === 0) {
    return <p className="text-sm text-muted-foreground">No replica observations.</p>;
  }
  return (
    <div className="overflow-hidden rounded-md border border-border">
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>Instance</TableHead>
            <TableHead>Source</TableHead>
            <TableHead>Heartbeat</TableHead>
            <TableHead>Running version</TableHead>
            <TableHead>Runtime state</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {replicas.map((replica) => (
            <TableRow key={replica.instanceId}>
              <TableCell><Code>{replica.instanceId}</Code></TableCell>
              <TableCell>
                <div className="space-y-1">
                  <Code>{replica.sourceVersion}</Code>
                  <div className="text-xs text-muted-foreground">
                    {replicaSourceLabel(replica.sourceStatus)}
                  </div>
                </div>
              </TableCell>
              <TableCell>{heartbeatAgeLabel(replica.heartbeatAgeSeconds, replica.fresh)}</TableCell>
              <TableCell><Code>{replica.appObservation?.runningVersion || "—"}</Code></TableCell>
              <TableCell>
                <div className="space-y-1">
                  <span>{replica.appObservation?.state || "unknown"}</span>
                  {replica.appObservation?.lastError ? (
                    <div className="max-w-72 text-xs text-destructive">{replica.appObservation.lastError}</div>
                  ) : null}
                </div>
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </div>
  );
}

function RegistryAppDetailPage() {
  const { app } = Route.useParams();
  const appQuery = useQuery(registryQueries.app(app));

  useEffect(() => {
    const timer = window.setTimeout(() => {
      void appQuery.refetch();
    }, 15_000);
    return () => window.clearTimeout(timer);
  }, [appQuery]);

  if (appQuery.isPending) {
    return <Skeleton className="h-64 w-full" />;
  }
  if (appQuery.isError || !appQuery.data) {
    return <p className="text-sm text-destructive">Failed to load registry app.</p>;
  }

  const detail = appQuery.data;
  const desired = detail.knownVersions.find((item) => item.version === detail.desiredVersion);
  const fleetRolloutNote = fleetRolloutSeparation(detail.fleetState, detail.rollout?.state);

  return (
    <div className="min-w-0 space-y-8">
      <div className="flex flex-wrap items-start justify-between gap-4">
        <div className="min-w-0 space-y-1">
          <h1 className="text-2xl font-normal tracking-tight">{detail.app}</h1>
          <p className="text-sm text-muted-foreground">Registry: {detail.registry}</p>
        </div>
        <RolloutBadge app={detail} />
      </div>

      <section className="min-w-0 space-y-4 overflow-hidden rounded-lg border border-border bg-card p-4">
        <SectionHeader>
          <SectionHeaderContent>
            <SectionHeaderTitle>Summary</SectionHeaderTitle>
            <SectionHeaderDescription>Fleet-known version and install metadata.</SectionHeaderDescription>
          </SectionHeaderContent>
        </SectionHeader>
        <dl className="grid min-w-0 gap-4 sm:grid-cols-2">
          <SummaryField label="Version">
            <Code className="block w-full">{detail.desiredVersion || "not installed"}</Code>
          </SummaryField>
          <SummaryField label="Latest published">
            <Code className="block w-full">{detail.latestPublished?.version || "—"}</Code>
          </SummaryField>
          <SummaryField label="Installed by">
            <span className="text-sm break-all">{desired?.installedBy || "—"}</span>
          </SummaryField>
          <SummaryField label="Installed at">
            <span className="text-sm">{formatTime(desired?.installedAt)}</span>
          </SummaryField>
        </dl>
      </section>

      <section className="space-y-4 rounded-lg border border-border bg-card p-4">
        <SectionHeader>
          <SectionHeaderContent>
            <SectionHeaderTitle>Current fleet</SectionHeaderTitle>
            <SectionHeaderDescription>
              Live runtime observations, independent from the historical rollout outcome.
            </SectionHeaderDescription>
          </SectionHeaderContent>
          <SectionHeaderActions>
            <FleetStateBadge fleet={detail.fleetState} />
          </SectionHeaderActions>
        </SectionHeader>
        <dl className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
          <SummaryField label="Capacity">
            <span className="text-sm">{fleetCapacityLabel(detail.fleetState)}</span>
          </SummaryField>
          <SummaryField label="Running desired">
            <span className="text-sm">
              {detail.fleetState.runningDesiredVersion}/{detail.fleetState.liveInstances}
            </span>
          </SummaryField>
          <SummaryField label="Current source">
            <Code className="block w-full">{detail.fleetState.sourceVersion || "unavailable"}</Code>
          </SummaryField>
          <SummaryField label="Desired version">
            <Code className="block w-full">{detail.fleetState.desiredVersion || "unavailable"}</Code>
          </SummaryField>
          <SummaryField label="Evaluation">
            <span className="text-sm">{fleetDiagnostic(detail.fleetState)}</span>
          </SummaryField>
        </dl>
        {fleetRolloutNote ? <p className="text-sm text-muted-foreground">{fleetRolloutNote}</p> : null}
      </section>

      <section className="space-y-4 rounded-lg border border-border bg-card p-4">
        <SectionHeader>
          <SectionHeaderContent>
            <SectionHeaderTitle>Rollout</SectionHeaderTitle>
          </SectionHeaderContent>
        </SectionHeader>
        {detail.rollout ? (
          <dl className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
            {[
              ["State", detail.rollout.state],
              ["Target source version", detail.rollout.targetSourceVersion || "—"],
              ["Created", formatTime(detail.rollout.createdAt)],
              ["Enrollment ends", formatTime(detail.rollout.enrollmentEndsAt)],
              ["Deadline", formatTime(detail.rollout.deadline)],
              ["Completed", formatTime(detail.rollout.completedAt)],
              ["Failed", formatTime(detail.rollout.failedAt)],
            ].map(([label, value]) => (
              <div key={label}>
                <dt className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">{label}</dt>
                <dd className="mt-1 text-sm">{value}</dd>
              </div>
            ))}
          </dl>
        ) : (
          <p className="text-sm text-muted-foreground">No rollout has started.</p>
        )}
      </section>

      <section className="space-y-5 rounded-lg border border-border bg-card p-4">
        <SectionHeader>
          <SectionHeaderContent>
            <SectionHeaderTitle>Replica observations</SectionHeaderTitle>
            <SectionHeaderDescription>
              Heartbeat freshness and app state by process. Superseded sources do not count toward fleet health.
            </SectionHeaderDescription>
          </SectionHeaderContent>
        </SectionHeader>
        <div className="space-y-2">
          <h3 className="text-sm font-medium">Fresh heartbeats</h3>
          <ReplicaTable replicas={detail.freshReplicas} />
        </div>
        <div className="space-y-2">
          <h3 className="text-sm font-medium">Stale heartbeats</h3>
          <ReplicaTable replicas={detail.staleReplicas} />
        </div>
      </section>

      <section className="space-y-4 rounded-lg border border-border bg-card p-4">
        <SectionHeader>
          <SectionHeaderContent>
            <SectionHeaderTitle>Replica pool</SectionHeaderTitle>
            <SectionHeaderDescription>
              Enrollment cohort totals for the current rollout (replicas that acknowledged before enrollment closed).
            </SectionHeaderDescription>
          </SectionHeaderContent>
        </SectionHeader>
        {!detail.rollout ? (
          <p className="text-sm text-muted-foreground">No rollout in progress.</p>
        ) : !detail.cohort || detail.cohort.acknowledged === 0 ? (
          <p className="text-sm text-muted-foreground">No replicas have joined the rollout cohort yet.</p>
        ) : (
          <dl className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
            <SummaryField label="Acknowledged">
              <span className="text-sm">{detail.cohort.acknowledged}</span>
            </SummaryField>
            <SummaryField label="Materialized">
              <span className="text-sm">{detail.cohort.materialized}</span>
            </SummaryField>
            <SummaryField label="Restarted">
              <span className="text-sm">{detail.cohort.restarted}</span>
            </SummaryField>
            <SummaryField label="Failed">
              <span className="text-sm">{detail.cohort.failed}</span>
            </SummaryField>
          </dl>
        )}
      </section>
    </div>
  );
}
