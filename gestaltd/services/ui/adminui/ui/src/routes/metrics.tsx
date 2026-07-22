import { useEffect, useState } from "react";
import { createFileRoute } from "@tanstack/react-router";
import { RefreshCcw } from "lucide-react";
import { Button } from "@/components/ui/button";
import {
  PageHeader,
  PageHeaderContent,
  PageHeaderDescription,
  PageHeaderTitle,
} from "@/components/ui/page-header";

export const Route = createFileRoute("/metrics")({
  component: MetricsPage,
});

function MetricsPage() {
  const [output, setOutput] = useState("");
  const [status, setStatus] = useState("Loading metrics...");
  const [refreshing, setRefreshing] = useState(false);

  async function refreshMetrics() {
    setRefreshing(true);
    setStatus("Loading metrics...");
    try {
      const response = await fetch("/metrics", {
        headers: { Accept: "text/plain" },
        cache: "no-store",
      });
      if (!response.ok) throw new Error(`Metrics request failed with HTTP ${response.status}`);
      setOutput(await response.text());
      setStatus(`Last refreshed ${new Date().toLocaleTimeString()}`);
    } catch (error) {
      setOutput("");
      setStatus(error instanceof Error ? error.message : "Failed to load metrics.");
    } finally {
      setRefreshing(false);
    }
  }

  useEffect(() => {
    void refreshMetrics();
  }, []);

  return (
    <div className="space-y-6">
      <PageHeader>
        <PageHeaderContent>
          <PageHeaderTitle>Metrics</PageHeaderTitle>
          <PageHeaderDescription>
            Inspect the live Prometheus telemetry exposed by this process.
          </PageHeaderDescription>
        </PageHeaderContent>
      </PageHeader>
      <div className="overflow-hidden rounded-lg border border-border bg-card">
        <div className="flex items-center justify-between gap-3 border-b border-border px-4 py-3">
          <span className="text-sm text-muted-foreground">{status}</span>
          <Button type="button" variant="outline" size="sm" onClick={() => void refreshMetrics()} disabled={refreshing}>
            <RefreshCcw className={refreshing ? "animate-spin motion-reduce:animate-none" : ""} />
            Refresh
          </Button>
        </div>
        <pre className="max-h-[calc(100vh-220px)] min-h-[480px] overflow-auto whitespace-pre-wrap p-4 text-sm">
          {output || (refreshing ? "" : "Click refresh to load metrics.")}
        </pre>
      </div>
    </div>
  );
}
