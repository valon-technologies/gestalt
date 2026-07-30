export type FleetState = {
  state: "healthy" | "converging" | "degraded" | "unknown";
  sourceVersion?: string;
  desiredVersion?: string;
  minimumHealthyInstances: number;
  liveInstances: number;
  runningDesiredVersion: number;
  mismatched: number;
  errors: number;
  heartbeatTtlSeconds: number;
  evaluatedAt: string;
};

export type FleetReplica = {
  instanceId: string;
  sourceVersion: string;
  currentSource: boolean;
  fresh: boolean;
  startedAt?: string;
  heartbeatAt: string;
  heartbeatAgeSeconds: number;
  appObservation: {
    state: "running" | "starting" | "not_running" | "error" | "unknown";
    desiredVersion?: string;
    runningVersion?: string;
    observedAt?: string;
    lastError?: string;
  };
};

export type RegistryAppSummary = {
  app: string;
  registry: string;
  desiredVersion?: string;
  rollout?: {
    version: string;
    state: string;
    targetSourceVersion?: string;
    createdAt: string;
    enrollmentEndsAt: string;
    deadline: string;
    completedAt?: string;
    failedAt?: string;
  };
  cohort?: {
    acknowledged: number;
    materialized: number;
    restarted: number;
    failed: number;
  };
  fleetState: FleetState;
};

export type RegistryAppDetail = RegistryAppSummary & {
  knownVersions: Array<{
    version: string;
    installedAt?: string;
    installedBy?: string;
  }>;
  latestPublished?: {
    version: string;
    publishedAt: string;
  };
  freshReplicas: FleetReplica[];
  staleReplicas: FleetReplica[];
};

export type MaterializationsResponse = {
  app: string;
  version: string;
  rolloutState?: string;
  materializations: Array<{
    instanceId: string;
    sourceVersion?: string;
    acknowledgedAt?: string | null;
    materializedAt?: string | null;
    stoppedAt?: string | null;
    restartedAt?: string | null;
    attemptCount: number;
    lastErrorMessage?: string;
    inCohort: boolean;
    converged: boolean;
  }>;
};
