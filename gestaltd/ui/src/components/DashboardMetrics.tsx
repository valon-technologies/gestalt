import type { OperationMetricsOverview } from "@/lib/api";

function formatPercent(value: number): string {
  return new Intl.NumberFormat("en-US", {
    style: "percent",
    maximumFractionDigits: 1,
  }).format(Number.isFinite(value) ? value : 0);
}

function formatCount(value: number): string {
  return new Intl.NumberFormat("en-US", {
    maximumFractionDigits: 0,
  }).format(value);
}

function formatLatency(value: number): string {
  return `${new Intl.NumberFormat("en-US", {
    maximumFractionDigits: 1,
  }).format(value)} ms`;
}

function formatThroughput(value: number): string {
  return `${new Intl.NumberFormat("en-US", {
    maximumFractionDigits: 2,
  }).format(value)}/s`;
}

function formatTimestamp(value?: string): string {
  if (!value) return "";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "";
  return new Intl.DateTimeFormat("en-US", {
    hour: "numeric",
    minute: "2-digit",
    month: "short",
    day: "numeric",
  }).format(date);
}

function metricValue(value?: number, formatter = formatCount): string {
  return typeof value === "number" ? formatter(value) : "--";
}

function MetricCard({
  label,
  value,
  detail,
}: {
  label: string;
  value: string;
  detail?: string;
}) {
  return (
    <div className="rounded-lg border border-alpha bg-base-100 p-4 dark:bg-surface">
      <span className="label-text">{label}</span>
      <p className="mt-2 text-2xl font-heading font-bold text-primary">
        {value}
      </p>
      {detail && <p className="mt-2 text-xs text-muted">{detail}</p>}
    </div>
  );
}

function MiniBars({
  buckets,
}: {
  buckets: NonNullable<OperationMetricsOverview["series"]>;
}) {
  if (buckets.length === 0) {
    return (
      <div className="rounded-lg border border-dashed border-alpha bg-base-100/60 px-4 py-8 text-sm text-muted dark:bg-surface/60">
        No buckets yet.
      </div>
    );
  }

  const recent = buckets.slice(-12);
  const peak = Math.max(...recent.map((bucket) => bucket.requests), 1);

  return (
    <div className="space-y-3">
      <div className="flex items-end gap-1 rounded-lg border border-alpha bg-base-100 p-3 dark:bg-surface">
        {recent.map((bucket) => {
          const height = Math.max(8, (bucket.requests / peak) * 100);
          return (
            <div
              key={bucket.start}
              className="flex-1"
              title={`${bucket.start}: ${bucket.requests} requests, ${bucket.errors} errors`}
            >
              <div className="flex h-32 items-end">
                <div
                  data-testid="metrics-bar"
                  className="w-full rounded-t-md bg-amber-500/80 dark:bg-amber-400/80"
                  style={{ height: `${height}%` }}
                />
              </div>
            </div>
          );
        })}
      </div>
      <div className="flex items-center justify-between text-[11px] uppercase tracking-widest text-faint">
        <span>{recent[0] ? formatTimestamp(recent[0].start) : ""}</span>
        <span>
          {recent[recent.length - 1]
            ? formatTimestamp(recent[recent.length - 1].start)
            : ""}
        </span>
      </div>
    </div>
  );
}

function BreakdownList({
  title,
  items,
}: {
  title: string;
  items?: OperationMetricsOverview["providers"];
}) {
  return (
    <div className="rounded-lg border border-alpha bg-base-100 p-4 dark:bg-surface">
      <div className="flex items-center justify-between">
        <span className="label-text">{title}</span>
        <span className="text-xs text-faint">{items?.length ?? 0} rows</span>
      </div>
      <div className="mt-4 space-y-3">
        {(items ?? []).length === 0 ? (
          <p className="text-sm text-muted">No activity yet.</p>
        ) : (
          (items ?? []).map((item) => (
            <div
              key={`${item.provider}:${item.operation || ""}`}
              className="flex items-start justify-between gap-4 border-b border-alpha/60 pb-3 last:border-0 last:pb-0"
            >
              <div>
                <p className="text-sm font-medium text-primary">
                  {item.provider}
                </p>
                {item.operation && (
                  <p className="mt-1 text-xs text-muted">{item.operation}</p>
                )}
              </div>
              <div className="text-right text-xs text-muted">
                <p>{formatCount(item.requests)} requests</p>
                <p>{formatPercent(item.error_rate)} errors</p>
                <p>{formatLatency(item.p95_latency_ms)} p95</p>
              </div>
            </div>
          ))
        )}
      </div>
    </div>
  );
}

export default function DashboardMetrics({
  metrics,
  error,
}: {
  metrics: OperationMetricsOverview | null;
  error: string | null;
}) {
  return (
    <section className="mt-10 overflow-hidden rounded-lg border border-alpha bg-base-100 shadow-card dark:bg-surface">
      <div className="border-b border-alpha/70 bg-gradient-to-r from-[#F7F1E8] to-transparent px-6 py-5 dark:from-[#211913]">
        <span className="label-text">Built-in metrics</span>
        <h2 className="mt-2 text-xl font-heading font-bold text-primary">
          Operation activity
        </h2>
        <p className="mt-2 text-sm text-muted">
          Rolling basic metrics from the default telemetry sink.
        </p>
      </div>

      <div className="space-y-6 px-6 py-6">
        {error && <p className="text-sm text-ember-500">{error}</p>}

        {!metrics ? (
          <div className="rounded-lg border border-dashed border-alpha bg-base-100/70 px-4 py-8 text-sm text-muted dark:bg-surface/70">
            Collecting built-in metrics.
          </div>
        ) : !metrics.enabled ? (
          <div className="rounded-lg border border-dashed border-alpha bg-base-100/70 px-4 py-8 text-sm text-muted dark:bg-surface/70">
            {metrics.reason ||
              "Built-in metrics are unavailable for the current telemetry provider."}
          </div>
        ) : (
          <>
            <div className="grid grid-cols-2 gap-4 lg:grid-cols-4">
              <MetricCard
                label="Requests"
                value={metricValue(metrics.summary.requests)}
                detail={
                  metrics.window_seconds
                    ? `Rolling ${Math.round(metrics.window_seconds / 60)}m window`
                    : undefined
                }
              />
              <MetricCard
                label="Errors"
                value={metricValue(metrics.summary.errors)}
                detail={`Error rate ${formatPercent(metrics.summary.error_rate)}`}
              />
              <MetricCard
                label="Avg latency"
                value={metricValue(
                  metrics.summary.avg_latency_ms,
                  formatLatency,
                )}
                detail={`p95 ${formatLatency(metrics.summary.p95_latency_ms)}`}
              />
              <MetricCard
                label="Throughput"
                value={metricValue(
                  metrics.summary.throughput_rps,
                  formatThroughput,
                )}
                detail="Requests per second across the rolling window"
              />
            </div>

            <div className="grid gap-4 lg:grid-cols-[1.4fr_1fr]">
              <div className="rounded-lg border border-alpha bg-base-100 p-4 dark:bg-surface">
                <div className="flex items-center justify-between">
                  <span className="label-text">Recent buckets</span>
                  <span className="text-xs text-faint">
                    {metrics.bucket_seconds
                      ? `${metrics.bucket_seconds}s buckets`
                      : "Rolling buckets"}
                  </span>
                </div>
                <div className="mt-4">
                  <MiniBars buckets={metrics.series ?? []} />
                </div>
              </div>

              <div className="space-y-4">
                <BreakdownList
                  title="Top providers"
                  items={metrics.providers}
                />
                <BreakdownList
                  title="Top operations"
                  items={metrics.operations}
                />
              </div>
            </div>

            {metrics.generated_at && (
              <p className="text-xs text-faint">
                Last refreshed {formatTimestamp(metrics.generated_at)}.
              </p>
            )}
          </>
        )}
      </div>
    </section>
  );
}
