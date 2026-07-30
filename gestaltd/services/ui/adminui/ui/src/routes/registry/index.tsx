import { useQuery } from "@tanstack/react-query";
import { createFileRoute, Link } from "@tanstack/react-router";
import { Code } from "@/components/ui/code";
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyTitle,
} from "@/components/ui/empty";
import {
  PageHeader,
  PageHeaderContent,
  PageHeaderDescription,
  PageHeaderTitle,
} from "@/components/ui/page-header";
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
import { FleetStateBadge, fleetCapacityLabel } from "@/features/registry/fleet-state";
import { RolloutBadge } from "@/features/registry/rollout-badge";

export const Route = createFileRoute("/registry/")({
  component: RegistryAppsPage,
});

function RegistryAppsPage() {
  const appsQuery = useQuery(registryQueries.apps());

  return (
    <div className="space-y-6">
      <PageHeader>
        <PageHeaderContent>
          <PageHeaderTitle>App Registry</PageHeaderTitle>
          <PageHeaderDescription>
            Current fleet health, desired versions, and historical rollout progress.
          </PageHeaderDescription>
        </PageHeaderContent>
      </PageHeader>

      {appsQuery.isPending ? (
        <Skeleton className="h-48 w-full" />
      ) : appsQuery.isError ? (
        <Empty>
          <EmptyHeader>
            <EmptyTitle>Failed to load registry apps</EmptyTitle>
            <EmptyDescription>
              {appsQuery.error instanceof Error ? appsQuery.error.message : "Unknown error"}
            </EmptyDescription>
          </EmptyHeader>
        </Empty>
      ) : appsQuery.data.length === 0 ? (
        <Empty>
          <EmptyHeader>
            <EmptyTitle>No registry-only apps</EmptyTitle>
            <EmptyDescription>No registry-only apps are configured in deploy config.</EmptyDescription>
          </EmptyHeader>
        </Empty>
      ) : (
        <div className="overflow-hidden rounded-lg border border-border bg-card">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>App</TableHead>
                <TableHead>Version</TableHead>
                <TableHead>Current fleet</TableHead>
                <TableHead>Rollout</TableHead>
                <TableHead>Cohort</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {appsQuery.data.map((app) => (
                <TableRow key={app.app}>
                  <TableCell>
                    <div className="space-y-1">
                      <Link
                        to="/registry/$app"
                        params={{ app: app.app }}
                        className="font-medium text-primary no-underline hover:underline"
                      >
                        {app.app}
                      </Link>
                      <div className="text-sm text-muted-foreground">{app.registry}</div>
                    </div>
                  </TableCell>
                  <TableCell>
                    <Code>{app.desiredVersion || "not installed"}</Code>
                  </TableCell>
                  <TableCell>
                    <div className="space-y-1">
                      <FleetStateBadge fleet={app.fleetState} />
                      <div className="text-xs text-muted-foreground">{fleetCapacityLabel(app.fleetState)}</div>
                    </div>
                  </TableCell>
                  <TableCell>
                    <RolloutBadge app={app} />
                  </TableCell>
                  <TableCell>
                    {app.cohort ? `${app.cohort.restarted}/${app.cohort.acknowledged} restarted` : "—"}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      )}
    </div>
  );
}
