(function () {
  const pollIntervalMs = 15000;
  const maxSamples = 12;
  const sessionEmailKey = "user_email";
  const metricNames = {
    requests: "gestaltd_operation_count_total",
    errors: "gestaltd_operation_error_count_total",
    durationSum: "gestaltd_operation_duration_seconds_sum",
    durationCount: "gestaltd_operation_duration_seconds_count",
    durationBucket: "gestaltd_operation_duration_seconds_bucket",
  };

  let previousSample = null;
  const throughputSamples = [];

  const els = {
    status: document.getElementById("status"),
    summary: document.getElementById("summary"),
    bars: document.getElementById("bars"),
    barsStart: document.getElementById("bars-start"),
    barsEnd: document.getElementById("bars-end"),
    throughputDetail: document.getElementById("throughput-detail"),
    providers: document.getElementById("providers"),
    operations: document.getElementById("operations"),
  };

  function setStatus(kind, text) {
    els.status.className = `status ${kind}`;
    els.status.textContent = text;
  }

  function number(value, digits) {
    if (!Number.isFinite(value)) {
      return "--";
    }
    return new Intl.NumberFormat("en-US", {
      maximumFractionDigits: digits,
    }).format(value);
  }

  function percent(value) {
    if (!Number.isFinite(value)) {
      return "--";
    }
    return new Intl.NumberFormat("en-US", {
      style: "percent",
      maximumFractionDigits: 1,
    }).format(value);
  }

  function escapeHTML(value) {
    return String(value)
      .replaceAll("&", "&amp;")
      .replaceAll("<", "&lt;")
      .replaceAll(">", "&gt;")
      .replaceAll('"', "&quot;")
      .replaceAll("'", "&#39;");
  }

  function latencySecondsToMs(value) {
    return Number.isFinite(value) ? value * 1000 : NaN;
  }

  function parseLabels(raw) {
    if (!raw) {
      return {};
    }
    const labels = {};
    let key = "";
    let value = "";
    let inValue = false;
    let escaping = false;

    for (let i = 0; i < raw.length; i += 1) {
      const char = raw[i];
      if (!inValue) {
        if (char === "=") {
          inValue = true;
          continue;
        }
        if (char === "," || char === " " || char === "\n") {
          continue;
        }
        key += char;
        continue;
      }

      if (value === "" && char === '"') {
        value = "__open__";
        continue;
      }

      if (escaping) {
        value += char;
        escaping = false;
        continue;
      }

      if (char === "\\") {
        escaping = true;
        continue;
      }

      if (char === '"') {
        labels[key] = value === "__open__" ? "" : value.replace(/^__open__/, "");
        key = "";
        value = "";
        inValue = false;
        continue;
      }

      value += char;
    }

    return labels;
  }

  function parsePrometheus(text) {
    const series = [];
    const lines = text.split("\n");
    for (const line of lines) {
      const trimmed = line.trim();
      if (!trimmed || trimmed.startsWith("#")) {
        continue;
      }

      const braceIndex = trimmed.indexOf("{");
      const spaceIndex = trimmed.lastIndexOf(" ");
      if (spaceIndex === -1) {
        continue;
      }

      let name = trimmed.slice(0, spaceIndex);
      let labels = {};
      if (braceIndex !== -1) {
        const closeIndex = trimmed.indexOf("}", braceIndex);
        if (closeIndex === -1) {
          continue;
        }
        name = trimmed.slice(0, braceIndex);
        labels = parseLabels(trimmed.slice(braceIndex + 1, closeIndex));
      }

      const value = Number(trimmed.slice(spaceIndex + 1));
      if (!Number.isFinite(value)) {
        continue;
      }

      series.push({ name, labels, value });
    }
    return series;
  }

  function ensureBreakdown(map, key, label) {
    if (!map.has(key)) {
      map.set(key, {
        label,
        requests: 0,
        errors: 0,
      });
    }
    return map.get(key);
  }

  function aggregateMetrics(series) {
    const providers = new Map();
    const operations = new Map();
    const buckets = new Map();
    let requestTotal = 0;
    let errorTotal = 0;
    let durationSum = 0;
    let durationCount = 0;

    for (const sample of series) {
      const provider = sample.labels.gestalt_provider || "unknown";
      const operation = sample.labels.gestalt_operation || "unknown";
      const providerStats = ensureBreakdown(providers, provider, provider);
      const operationKey = `${provider}:${operation}`;
      const operationStats = ensureBreakdown(
        operations,
        operationKey,
        `${provider} / ${operation}`,
      );

      switch (sample.name) {
        case metricNames.requests:
          requestTotal += sample.value;
          providerStats.requests += sample.value;
          operationStats.requests += sample.value;
          break;
        case metricNames.errors:
          errorTotal += sample.value;
          providerStats.errors += sample.value;
          operationStats.errors += sample.value;
          break;
        case metricNames.durationSum:
          durationSum += sample.value;
          break;
        case metricNames.durationCount:
          durationCount += sample.value;
          break;
        case metricNames.durationBucket: {
          const upper = sample.labels.le || "+Inf";
          buckets.set(upper, (buckets.get(upper) || 0) + sample.value);
          break;
        }
        default:
          break;
      }
    }

    return {
      requestTotal,
      errorTotal,
      durationSum,
      durationCount,
      buckets,
      providers: Array.from(providers.values())
        .sort((a, b) => b.requests - a.requests)
        .slice(0, 5),
      operations: Array.from(operations.values())
        .sort((a, b) => b.requests - a.requests)
        .slice(0, 5),
    };
  }

  function histogramQuantile(buckets, quantile) {
    const ordered = Array.from(buckets.entries())
      .map(([upper, value]) => ({
        upper: upper === "+Inf" ? Infinity : Number(upper),
        value,
      }))
      .filter((bucket) => Number.isFinite(bucket.value))
      .sort((a, b) => a.upper - b.upper);

    if (ordered.length === 0) {
      return NaN;
    }

    const total = ordered[ordered.length - 1].value;
    if (total <= 0) {
      return NaN;
    }

    const target = total * quantile;
    let previousCount = 0;
    let previousUpper = 0;

    for (const bucket of ordered) {
      if (bucket.value >= target) {
        if (!Number.isFinite(bucket.upper)) {
          return previousUpper;
        }
        const bucketCount = bucket.value - previousCount;
        if (bucketCount <= 0) {
          return bucket.upper;
        }
        const position = (target - previousCount) / bucketCount;
        return previousUpper + (bucket.upper - previousUpper) * position;
      }
      previousCount = bucket.value;
      previousUpper = Number.isFinite(bucket.upper) ? bucket.upper : previousUpper;
    }

    return previousUpper;
  }

  function summarize(snapshot) {
    const avgLatencyMs =
      snapshot.durationCount > 0
        ? latencySecondsToMs(snapshot.durationSum / snapshot.durationCount)
        : NaN;
    const p95LatencyMs = latencySecondsToMs(
      histogramQuantile(snapshot.buckets, 0.95),
    );
    const errorRate =
      snapshot.requestTotal > 0 ? snapshot.errorTotal / snapshot.requestTotal : 0;

    return {
      requests: snapshot.requestTotal,
      errors: snapshot.errorTotal,
      errorRate,
      avgLatencyMs,
      p95LatencyMs,
    };
  }

  function appendThroughputSample(snapshot, now) {
    let throughput = NaN;

    if (previousSample) {
      const elapsedSeconds = (now - previousSample.recordedAt) / 1000;
      const deltaRequests =
        snapshot.requestTotal >= previousSample.requestTotal
          ? snapshot.requestTotal - previousSample.requestTotal
          : snapshot.requestTotal;

      if (elapsedSeconds > 0) {
        throughput = deltaRequests / elapsedSeconds;
        throughputSamples.push({
          recordedAt: now,
          value: throughput,
        });
        while (throughputSamples.length > maxSamples) {
          throughputSamples.shift();
        }
      }
    }

    previousSample = {
      recordedAt: now,
      requestTotal: snapshot.requestTotal,
    };

    return throughput;
  }

  function renderSummary(summary, throughput, scrapedAt) {
    const cards = [
      {
        label: "Requests",
        value: number(summary.requests, 0),
        detail: "Process lifetime total",
      },
      {
        label: "Errors",
        value: number(summary.errors, 0),
        detail: `Error rate ${percent(summary.errorRate)}`,
      },
      {
        label: "Avg latency",
        value: Number.isFinite(summary.avgLatencyMs)
          ? `${number(summary.avgLatencyMs, 1)} ms`
          : "--",
        detail: Number.isFinite(summary.p95LatencyMs)
          ? `p95 ${number(summary.p95LatencyMs, 1)} ms`
          : "p95 unavailable",
      },
      {
        label: "Throughput",
        value: Number.isFinite(throughput) ? `${number(throughput, 2)}/s` : "--",
        detail: "Computed from the last two scrapes",
      },
      {
        label: "Scraped",
        value: new Intl.DateTimeFormat("en-US", {
          hour: "numeric",
          minute: "2-digit",
          second: "2-digit",
        }).format(scrapedAt),
        detail: "Browser-refreshed every 15 seconds",
      },
      {
        label: "Source",
        value: "/metrics",
        detail: "No extra admin JSON endpoint",
      },
    ];

    els.summary.innerHTML = cards
      .map(
        (card) => `
          <article class="summary-card">
            <p class="eyebrow">${card.label}</p>
            <strong>${card.value}</strong>
            <span>${card.detail}</span>
          </article>
        `,
      )
      .join("");
  }

  function renderBars() {
    if (throughputSamples.length === 0) {
      els.bars.innerHTML = '<div class="empty">Poll once more to see recent request throughput.</div>';
      els.barsStart.textContent = "Now";
      els.barsEnd.textContent = "Now";
      els.throughputDetail.textContent = "Waiting for a second scrape...";
      return;
    }

    const peak = Math.max(...throughputSamples.map((sample) => sample.value), 0.0001);
    els.bars.innerHTML = throughputSamples
      .map((sample) => {
        const height = Math.max(8, (sample.value / peak) * 100);
        return `
          <div class="bar" title="${number(sample.value, 2)}/s">
            <div class="bar-fill" style="height:${height}%"></div>
            <span class="bar-label">${number(sample.value, 1)}</span>
          </div>
        `;
      })
      .join("");

    const first = throughputSamples[0].recordedAt;
    const last = throughputSamples[throughputSamples.length - 1].recordedAt;
    els.barsStart.textContent = new Intl.DateTimeFormat("en-US", {
      hour: "numeric",
      minute: "2-digit",
    }).format(first);
    els.barsEnd.textContent = new Intl.DateTimeFormat("en-US", {
      hour: "numeric",
      minute: "2-digit",
    }).format(last);
    els.throughputDetail.textContent = "Recent throughput since this page was opened";
  }

  function renderBreakdown(container, rows, noun) {
    if (rows.length === 0) {
      container.innerHTML = `<div class="empty">No ${noun} recorded yet.</div>`;
      return;
    }

    container.innerHTML = rows
      .map((row) => {
        const errorRate = row.requests > 0 ? row.errors / row.requests : 0;
        return `
              <div class="row">
            <div>
              <div class="row-name">${escapeHTML(row.label)}</div>
              <div class="row-meta">${number(row.errors, 0)} errors</div>
            </div>
            <div class="row-value">
              <div>${number(row.requests, 0)} requests</div>
              <div class="row-meta">${percent(errorRate)} error rate</div>
            </div>
          </div>
        `;
      })
      .join("");
  }

  async function refresh() {
    try {
      const response = await fetch("/metrics", {
        credentials: "include",
        headers: { Accept: "text/plain" },
      });

      if (response.status === 401) {
        try {
          window.localStorage.removeItem(sessionEmailKey);
        } catch {}
        window.location.href = "/login";
        return;
      }

      if (!response.ok) {
        throw new Error(`Failed to fetch /metrics (${response.status})`);
      }

      const text = await response.text();
      const snapshot = aggregateMetrics(parsePrometheus(text));
      const summary = summarize(snapshot);
      const scrapedAt = new Date();
      const throughput = appendThroughputSample(snapshot, scrapedAt);

      renderSummary(summary, throughput, scrapedAt);
      renderBars();
      renderBreakdown(els.providers, snapshot.providers, "provider activity");
      renderBreakdown(els.operations, snapshot.operations, "operation activity");

      setStatus(
        "ok",
        `Last refreshed ${new Intl.DateTimeFormat("en-US", {
          hour: "numeric",
          minute: "2-digit",
          second: "2-digit",
        }).format(scrapedAt)}. Totals are cumulative since process start.`,
      );
    } catch (error) {
      const message =
        error instanceof Error ? error.message : "Failed to load Prometheus metrics";
      setStatus("error", message);
    }
  }

  refresh();
  window.setInterval(refresh, pollIntervalMs);
})();
