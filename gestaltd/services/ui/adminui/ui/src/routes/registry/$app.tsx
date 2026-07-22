import { useEffect, type ReactNode } from "react";
import { useQuery } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import { Code } from "@/components/ui/code";
import {
  SectionHeader,
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
import { registryQueries } from "@/features/registry/queries";
import { RolloutBadge } from "@/features/registry/rollout-badge";

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

function RegistryAppDetailPage() {
  const { app } = Route.useParams();
  const appQuery = useQuery(registryQueries.app(app));
  const materializationsQuery = useQuery({
    ...registryQueries.materializations(app),
    enabled: Boolean(appQuery.data?.rollout),
  });

  const active =
    appQuery.data?.rollout?.state === "enrolling" || appQuery.data?.rollout?.state === "restarting";

  useEffect(() => {
    if (!active) return undefined;
    const timer = window.setTimeout(() => {
      void appQuery.refetch();
      void materializationsQuery.refetch();
    }, 12_000);
    return () => window.clearTimeout(timer);
  }, [active, appQuery, materializationsQuery]);

  if (appQuery.isPending) {
    return <Skeleton className="h-64 w-full" />;
  }
  if (appQuery.isError || !appQuery.data) {
    return <p className="text-sm text-destructive">Failed to load registry app.</p>;
  }

  const detail = appQuery.data;
  const desired = detail.knownVersions.find((item) => item.version === detail.desiredVersion);
  const cohortReplicas =
    materializationsQuery.data?.materializations.filter((row) => row.inCohort) ?? [];

  return (
    <div className="space-y-8">
      <div className="flex flex-wrap items-start justify-between gap-4">
        <div className="space-y-1">
          <h1 className="text-2xl font-normal tracking-tight">{detail.app}</h1>
          <p className="text-sm text-muted-foreground">Registry: {detail.registry}</p>
        </div>
        <RolloutBadge app={detail} />
      </div>

      <section className="space-y-4 rounded-lg border border-border bg-card p-4">
        <SectionHeader>
          <SectionHeaderContent>
            <SectionHeaderTitle>Summary</SectionHeaderTitle>
            <SectionHeaderDescription>Fleet-known version and install metadata.</SectionHeaderDescription>
          </SectionHeaderContent>
        </SectionHeader>
        <dl className="grid gap-4 sm:grid-cols-2">
          <SummaryField label="Version">
            <Code>{detail.desiredVersion || "not installed"}</Code>
          </SummaryField>
          <SummaryField label="Latest published">
            <Code>{detail.latestPublished?.version || "—"}</Code>
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
            <SectionHeaderTitle>Rollout</SectionHeaderTitle>
          </SectionHeaderContent>
        </SectionHeader>
        {detail.rollout ? (
          <dl className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
            {[
              ["State", detail.rollout.state],
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

      <section className="space-y-4 rounded-lg border border-border bg-card p-4">
        <SectionHeader>
          <SectionHeaderContent>
            <SectionHeaderTitle>Replica pool</SectionHeaderTitle>
            <SectionHeaderDescription>
              Enrollment cohort totals for replicas that acknowledged before enrollment closed.
            </SectionHeaderDescription>
          </SectionHeaderContent>
        </SectionHeader>
        {!detail.rollout ? (
          <p className="text-sm text-muted-foreground">No rollout in progress.</p>
        ) : !detail.cohort || detail.cohort.acknowledged === 0 ? (
          <p className="text-sm text-muted-foreground">No replicas have joined the rollout cohort yet.</p>
        ) : (
          <div className="space-y-4">
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
            {materializationsQuery.isPending ? (
              <Skeleton className="h-24 w-full" />
            ) : cohortReplicas.length > 0 ? (
              <details className="rounded-lg border border-border">
                <summary className="cursor-pointer px-4 py-3 text-sm font-medium text-foreground">
                  Cohort replicas ({cohortReplicas.length})
                </summary>
                <div className="border-t border-border">
                  <Table>
                    <TableHeader>
                      <TableRow>
                        <TableHead>Instance</TableHead>
                        <TableHead>Restarted</TableHead>
                        <TableHead>Attempts</TableHead>
                        <TableHead>Last error</TableHead>
                      </TableRow>
                    </TableHeader>
                    <TableBody>
                      {cohortReplicas.map((row) => (
                        <TableRow key={row.instanceId}>
                          <TableCell className="min-w-0">
                            <Code>{row.instanceId}</Code>
                          </TableCell>
                          <TableCell>{formatTime(row.restartedAt)}</TableCell>
                          <TableCell>{row.attemptCount}</TableCell>
                          <TableCell className="min-w-0 break-all text-sm">
                            {row.lastErrorMessage || "—"}
                          </TableCell>
                        </TableRow>
                      ))}
                    </TableBody>
                  </Table>
                </div>
              </details>
            ) : null}
          </div>
        )}
      </section>
    </div>
  );
}
