export type RegistryAppSummary = {
  app: string;
  registry: string;
  desiredVersion?: string;
  rollout?: {
    version: string;
    state: string;
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
};

export type MaterializationsResponse = {
  app: string;
  version: string;
  rolloutState?: string;
  materializations: Array<{
    instanceId: string;
    acknowledgedAt?: string | null;
    materializedAt?: string | null;
    stoppedAt?: string | null;
    restartedAt?: string | null;
    attemptCount: number;
    lastErrorMessage?: string;
  }>;
};
