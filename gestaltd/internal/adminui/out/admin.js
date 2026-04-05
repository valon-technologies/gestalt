(function () {
  const METRIC_LINE =
    /^([a-zA-Z_:][a-zA-Z0-9_:]*)(\{[^}]*\})?\s+([+-]?(?:\d+(?:\.\d+)?|\.\d+)(?:[eE][+-]?\d+)?|NaN|[+-]?Inf)$/;
  const REFRESH_INTERVAL_MS = 15000;
  const HISTORY_LIMIT = Math.ceil((60 * 60 * 1000) / REFRESH_INTERVAL_MS);
  const charts = new Map();
  const history = [];
  let latestSnapshot = null;

  function byId(id) {
    return document.getElementById(id);
  }

  function setStatus(message, kind) {
    byId("status").className = kind ? `status ${kind}` : "status";
    byId("status").textContent = message;
  }

  function formatCount(value) {
    return new Intl.NumberFormat("en-US", { maximumFractionDigits: 0 }).format(
      Number.isFinite(value) ? value : 0,
    );
  }

  function formatDuration(seconds) {
    if (!Number.isFinite(seconds) || seconds < 0) return "--";
    const ms = seconds * 1000;
    return ms < 1000
      ? `${ms.toFixed(ms >= 100 ? 0 : 1)} ms`
      : `${seconds.toFixed(seconds >= 10 ? 1 : 2)} s`;
  }

  function formatPercent(value) {
    return Number.isFinite(value) ? `${value.toFixed(value >= 10 ? 1 : 2)}%` : "--";
  }

  function formatPerMinute(value) {
    return Number.isFinite(value) ? `${value.toFixed(value >= 10 ? 0 : 1)}/min` : "--";
  }

  function formatMilliseconds(value, digits) {
    return Number.isFinite(value) ? `${value.toFixed(digits)} ms` : "--";
  }

  function formatClock(value) {
    return new Date(value).toLocaleTimeString([], {
      hour: "numeric",
      minute: "2-digit",
    });
  }

  function parseLabels(raw) {
    const source =
      raw && raw[0] === "{" && raw[raw.length - 1] === "}" ? raw.slice(1, -1) : raw || "";
    const labels = {};
    const matcher = /([^=,]+)="((?:\\.|[^"])*)"/g;
    let match;
    while ((match = matcher.exec(source)) !== null) {
      labels[match[1]] = match[2].replace(/\\([\\n"])/g, function (_, escaped) {
        if (escaped === "n") return "\n";
        return escaped;
      });
    }
    return labels;
  }

  function percentileFromBuckets(buckets, quantile) {
    const points = Array.from(buckets.entries())
      .map(function (entry) {
        return [entry[0] === "+Inf" ? Number.POSITIVE_INFINITY : Number(entry[0]), entry[1]];
      })
      .filter(function (entry) {
        return Number.isFinite(entry[1]) && !Number.isNaN(entry[0]);
      })
      .sort(function (left, right) {
        return left[0] - right[0];
      });

    if (points.length === 0) return NaN;

    const total = points[points.length - 1][1];
    if (!Number.isFinite(total) || total <= 0) return NaN;

    const target = total * quantile;
    let lastFinite = NaN;
    for (const point of points) {
      if (Number.isFinite(point[0])) lastFinite = point[0];
      if (point[1] >= target) return Number.isFinite(point[0]) ? point[0] : lastFinite;
    }
    return lastFinite;
  }

  function parseMetrics(text) {
    const snapshot = {
      requests: 0,
      errors: 0,
      durationSum: 0,
      durationCount: 0,
      avgLatencySeconds: NaN,
      p95LatencySeconds: NaN,
      buckets: new Map(),
      providers: new Map(),
    };

    for (const rawLine of text.split("\n")) {
      const line = rawLine.trim();
      if (!line || line.startsWith("#")) continue;

      const match = line.match(METRIC_LINE);
      if (!match) continue;

      const metric = match[1];
      const labels = parseLabels(match[2] || "");
      const value = Number(match[3]);
      if (!Number.isFinite(value)) continue;

      if (metric === "gestaltd_operation_count_total") {
        const provider = labels.gestalt_provider || "unknown";
        if (!snapshot.providers.has(provider)) {
          snapshot.providers.set(provider, { label: provider, requests: 0, errors: 0 });
        }
        const row = snapshot.providers.get(provider);
        snapshot.requests += value;
        row.requests += value;
      } else if (metric === "gestaltd_operation_error_count_total") {
        const provider = labels.gestalt_provider || "unknown";
        if (!snapshot.providers.has(provider)) {
          snapshot.providers.set(provider, { label: provider, requests: 0, errors: 0 });
        }
        const row = snapshot.providers.get(provider);
        snapshot.errors += value;
        row.errors += value;
      } else if (metric === "gestaltd_operation_duration_seconds_sum") {
        snapshot.durationSum += value;
      } else if (metric === "gestaltd_operation_duration_seconds_count") {
        snapshot.durationCount += value;
      } else if (metric === "gestaltd_operation_duration_seconds_bucket") {
        const le = labels.le || "+Inf";
        snapshot.buckets.set(le, (snapshot.buckets.get(le) || 0) + value);
      }
    }

    snapshot.avgLatencySeconds =
      snapshot.durationCount > 0 ? snapshot.durationSum / snapshot.durationCount : NaN;
    snapshot.p95LatencySeconds = percentileFromBuckets(snapshot.buckets, 0.95);
    snapshot.providers = Array.from(snapshot.providers.values()).sort(function (left, right) {
      return right.requests - left.requests;
    });
    return snapshot;
  }

  function theme() {
    const styles = window.getComputedStyle(document.documentElement);
    return {
      brand: styles.getPropertyValue("--brand").trim(),
      brandSoft: styles.getPropertyValue("--brand-soft").trim(),
      border: styles.getPropertyValue("--border").trim(),
      foreground: styles.getPropertyValue("--foreground").trim(),
      muted: styles.getPropertyValue("--muted").trim(),
      faint: styles.getPropertyValue("--faint").trim(),
      danger: styles.getPropertyValue("--danger").trim(),
      success: styles.getPropertyValue("--success").trim(),
      surface: styles.getPropertyValue("--surface").trim(),
      surfaceRaised: styles.getPropertyValue("--surface-raised").trim(),
    };
  }

  function disposeChart(id) {
    const chart = charts.get(id);
    if (chart) {
      chart.dispose();
      charts.delete(id);
    }
  }

  function ensureChart(id) {
    const container = byId(id);
    if (!window.echarts) return null;
    const current = charts.get(id);
    if (current && !current.isDisposed()) return current;
    container.textContent = "";
    container.dataset.chartState = "ready";
    const chart = window.echarts.init(container, null, { renderer: "canvas" });
    charts.set(id, chart);
    return chart;
  }

  function renderMessage(container, message) {
    container.textContent = "";
    const note = document.createElement("p");
    note.className = "metric-note";
    note.textContent = message;
    container.appendChild(note);
  }

  function renderChartMessage(id, message) {
    disposeChart(id);
    const container = byId(id);
    container.dataset.chartState = "empty";
    renderMessage(container, message);
  }

  function renderProviderSummary(items, message) {
    const container = byId("provider-bars");
    if (message) {
      renderMessage(container, message);
      return;
    }
    if (items.length === 0) {
      renderMessage(container, "No operation metrics have been emitted yet.");
      return;
    }

    const top = items.slice(0, 5);
    const peak = Math.max.apply(
      null,
      top.map(function (item) {
        return item.requests;
      }).concat(0),
    );

    container.textContent = "";
    for (const item of top) {
      const row = document.createElement("div");
      row.innerHTML =
        '<div class="bar-copy"><span class="bar-name"></span><span class="bar-value"></span></div>' +
        '<div class="bar-track"><div class="bar-fill"></div></div><div class="bar-meta"></div>';
      row.querySelector(".bar-name").textContent = item.label;
      row.querySelector(".bar-value").textContent = `${formatCount(item.requests)} requests`;
      row.querySelector(".bar-fill").style.width = `${peak > 0 ? (item.requests / peak) * 100 : 0}%`;
      row.querySelector(".bar-meta").textContent =
        `${formatCount(item.errors)} errors · ` +
        `${formatPercent(item.requests > 0 ? (item.errors / item.requests) * 100 : 0)}`;
      container.appendChild(row);
    }
  }

  function positiveDelta(current, previous) {
    if (!Number.isFinite(current)) return 0;
    if (!Number.isFinite(previous)) return current;
    return current >= previous ? current - previous : current;
  }

  function recordHistory(snapshot, timestamp) {
    const previous = history[history.length - 1];
    let requestsPerMinute = 0;
    let errorsPerMinute = 0;

    if (previous) {
      const elapsedSeconds = Math.max((timestamp - previous.timestamp) / 1000, 1);
      requestsPerMinute =
        (positiveDelta(snapshot.requests, previous.requestsTotal) / elapsedSeconds) * 60;
      errorsPerMinute =
        (positiveDelta(snapshot.errors, previous.errorsTotal) / elapsedSeconds) * 60;
    }

    history.push({
      timestamp,
      requestsTotal: snapshot.requests,
      errorsTotal: snapshot.errors,
      requestsPerMinute,
      errorsPerMinute,
      avgLatencyMs: Number.isFinite(snapshot.avgLatencySeconds)
        ? snapshot.avgLatencySeconds * 1000
        : null,
      p95LatencyMs: Number.isFinite(snapshot.p95LatencySeconds)
        ? snapshot.p95LatencySeconds * 1000
        : null,
    });

    while (history.length > HISTORY_LIMIT) {
      history.shift();
    }
  }

  function activitySeries(key) {
    return history.map(function (entry) {
      return [entry.timestamp, entry[key]];
    });
  }

  function renderTimeSeriesChart(id, options) {
    if (!window.echarts) {
      renderChartMessage(id, "ECharts failed to load.");
      return;
    }

    const chart = ensureChart(id);
    const colors = theme();
    chart.setOption(
      {
        animation: false,
        aria: { enabled: true },
        color: options.colors(colors),
        grid: { left: 48, right: 18, top: 42, bottom: 24 },
        legend: {
          top: 8,
          textStyle: { color: colors.muted },
          itemWidth: 10,
          itemHeight: 10,
        },
        tooltip: {
          trigger: "axis",
          backgroundColor: colors.surfaceRaised,
          borderColor: colors.border,
          textStyle: { color: colors.foreground },
          valueFormatter: options.tooltipFormatter,
        },
        xAxis: {
          type: "time",
          axisLine: { lineStyle: { color: colors.border } },
          axisLabel: {
            color: colors.faint,
            formatter: formatClock,
          },
          splitLine: { show: false },
        },
        yAxis: {
          type: "value",
          axisLine: { show: false },
          axisLabel: {
            color: colors.faint,
            formatter: options.axisFormatter,
          },
          splitLine: { lineStyle: { color: colors.border } },
        },
        series: options.series,
      },
      true,
    );
  }

  function renderActivityChart() {
    renderTimeSeriesChart("activity-chart", {
      colors: function (colors) {
        return [colors.brand, colors.danger];
      },
      tooltipFormatter: function (value) {
        return formatPerMinute(Number(value));
      },
      axisFormatter: function (value) {
        return formatPerMinute(Number(value));
      },
      series: [
        {
          name: "Requests/min",
          type: "line",
          smooth: true,
          showSymbol: false,
          areaStyle: { opacity: 0.12 },
          lineStyle: { width: 2 },
          data: activitySeries("requestsPerMinute"),
        },
        {
          name: "Errors/min",
          type: "line",
          smooth: true,
          showSymbol: false,
          lineStyle: { width: 2 },
          data: activitySeries("errorsPerMinute"),
        },
      ],
    });
  }

  function renderLatencyChart() {
    renderTimeSeriesChart("latency-chart", {
      colors: function (colors) {
        return [colors.success, colors.brandSoft];
      },
      tooltipFormatter: function (value) {
        const digits = Number(value) >= 100 ? 0 : 1;
        return formatMilliseconds(Number(value), digits);
      },
      axisFormatter: function (value) {
        return formatMilliseconds(Number(value), 0);
      },
      series: [
        {
          name: "Average",
          type: "line",
          smooth: true,
          showSymbol: false,
          lineStyle: { width: 2 },
          data: activitySeries("avgLatencyMs"),
        },
        {
          name: "P95",
          type: "line",
          smooth: true,
          showSymbol: false,
          lineStyle: { width: 2 },
          data: activitySeries("p95LatencyMs"),
        },
      ],
    });
  }

  function renderProviderChart(items) {
    if (items.length === 0) {
      renderChartMessage("provider-chart", "No operation metrics have been emitted yet.");
      return;
    }
    if (!window.echarts) {
      renderChartMessage("provider-chart", "ECharts failed to load.");
      return;
    }

    const top = items.slice(0, 5);
    const chart = ensureChart("provider-chart");
    const colors = theme();
    chart.setOption(
      {
        animation: false,
        aria: { enabled: true },
        grid: { left: 140, right: 20, top: 20, bottom: 20 },
        tooltip: {
          trigger: "axis",
          axisPointer: { type: "shadow" },
          backgroundColor: colors.surfaceRaised,
          borderColor: colors.border,
          textStyle: { color: colors.foreground },
          formatter: function (params) {
            const point = params[0];
            const item = top[point.dataIndex];
            return `${item.label}<br>${formatCount(item.requests)} requests<br>${formatCount(item.errors)} errors`;
          },
        },
        xAxis: {
          type: "value",
          axisLine: { show: false },
          axisLabel: { color: colors.faint },
          splitLine: { lineStyle: { color: colors.border } },
        },
        yAxis: {
          type: "category",
          inverse: true,
          data: top.map(function (item) {
            return item.label;
          }),
          axisLine: { show: false },
          axisTick: { show: false },
          axisLabel: { color: colors.foreground },
        },
        series: [
          {
            type: "bar",
            data: top.map(function (item) {
              return item.requests;
            }),
            barWidth: 20,
            itemStyle: {
              borderRadius: [0, 999, 999, 0],
              color: colors.brand,
            },
            label: {
              show: true,
              position: "right",
              color: colors.muted,
              formatter: function (params) {
                return formatCount(Number(params.value));
              },
            },
          },
        ],
      },
      true,
    );
  }

  function renderCharts(snapshot) {
    renderActivityChart();
    renderLatencyChart();
    renderProviderChart(snapshot.providers);
    renderProviderSummary(snapshot.providers);
  }

  function clearDashboard(message) {
    byId("summary-requests").textContent = "--";
    byId("summary-errors").textContent = "--";
    byId("summary-avg-latency").textContent = "--";
    byId("summary-p95-latency").textContent = "--";
    renderChartMessage("activity-chart", message);
    renderChartMessage("latency-chart", message);
    renderChartMessage("provider-chart", message);
    renderProviderSummary([], message);
  }

  async function refresh() {
    try {
      const response = await fetch("/metrics", {
        credentials: "include",
        headers: { Accept: "text/plain" },
      });

      if (response.status === 401) {
        try {
          window.localStorage.removeItem("user_email");
        } catch {}
        window.location.replace("/login");
        return;
      }

      const contentType = (response.headers.get("content-type") || "").toLowerCase();
      const body = await response.text();

      if (!response.ok) {
        throw new Error(body.trim() || `Failed to fetch /metrics (${response.status})`);
      }
      if (contentType.includes("text/html")) {
        throw new Error("Prometheus metrics are unavailable.");
      }
      if (!window.echarts) {
        throw new Error("ECharts failed to load.");
      }

      const snapshot = parseMetrics(body);
      latestSnapshot = snapshot;
      recordHistory(snapshot, Date.now());
      byId("metrics-output").textContent = body;
      byId("summary-requests").textContent = formatCount(snapshot.requests);
      byId("summary-errors").textContent = formatCount(snapshot.errors);
      byId("summary-avg-latency").textContent = formatDuration(snapshot.avgLatencySeconds);
      byId("summary-p95-latency").textContent = formatDuration(snapshot.p95LatencySeconds);
      renderCharts(snapshot);
      setStatus(`Last refreshed ${new Date().toLocaleTimeString()}`, "ok");
    } catch (error) {
      const message = error instanceof Error ? error.message : "Failed to load Prometheus metrics";
      byId("metrics-output").textContent = message;
      clearDashboard(message);
      setStatus(message, "error");
    }
  }

  function resizeCharts() {
    charts.forEach(function (chart) {
      chart.resize();
    });
  }

  clearDashboard("Loading metrics...");
  refresh();
  window.setInterval(refresh, REFRESH_INTERVAL_MS);
  window.addEventListener("resize", resizeCharts);
  window.matchMedia("(prefers-color-scheme: dark)").addEventListener("change", function () {
    if (latestSnapshot) {
      renderCharts(latestSnapshot);
      resizeCharts();
    }
  });
})();
