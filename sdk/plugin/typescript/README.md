# Gestalt Plugin SDK for TypeScript

This is a thin helper for implementing a Gestalt subprocess plugin in
TypeScript.

```ts
import { createPlugin } from "./index";

createPlugin({
  pluginInfo: { name: "loan-enrichment", version: "0.1.0" },
  provider: {
    displayName: "Loan Enrichment",
    description: "Fetches enrichment data for a loan",
    connectionMode: "user",
    operations: [
      {
        name: "enrich_loan",
        description: "Fetch enrichment data",
        method: "POST",
        parameters: [{ name: "loan_id", type: "string", required: true }],
      },
    ],
  },
  async execute(request) {
    return {
      status: 200,
      body: JSON.stringify({ operation: request.operation, params: request.params }),
    };
  },
}).serve().catch((error) => {
  console.error(error);
  process.exit(1);
});
```

The SDK reads framed JSON-RPC messages from stdin and writes responses to
stdout. Use stderr for logs.

