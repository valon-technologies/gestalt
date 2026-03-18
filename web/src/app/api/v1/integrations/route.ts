import { NextResponse } from "next/server";
import { connectedIntegrations } from "../_mock-state";

const mockIntegrations = [
  { name: "acme_crm", display_name: "Acme CRM", description: "Example CRM integration" },
  { name: "test_analytics", display_name: "Test Analytics", description: "Example analytics integration" },
  { name: "sample_storage", display_name: "Sample Storage", description: "Example storage integration" },
];

export async function GET() {
  const result = mockIntegrations.map((i) => ({
    ...i,
    connected: connectedIntegrations.has(i.name),
  }));
  return NextResponse.json(result);
}
