export default {
  // App-router page; lives in the navbar, not the docs sidebar.
  registry: { display: "hidden" },
  index: "Overview",
  install: "Install",
  "getting-started": "Getting Started",
  "local-development": "Local Development",
  "-- server": { type: "separator", title: "Server" },
  providers: "Providers",
  applications: "Applications",
  "service-accounts": "Service Accounts",
  security: "Security",
  observability: "Observability",
  "audit-logging": "Audit Logging",
  deploy: "Deploy",
  "-- clients": { type: "separator", title: "Clients" },
  client: {
    display: "children",
  },
  "-- advanced": { type: "separator", title: "Advanced" },
  architecture: "Architecture",
  reference: "Reference",
  troubleshooting: "Troubleshooting",
};
