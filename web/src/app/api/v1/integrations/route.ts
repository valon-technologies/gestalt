import { NextResponse } from "next/server";
import { connectedIntegrations } from "../_mock-state";

const mockIntegrations = [
  { name: "acme_crm", display_name: "Acme CRM", description: "Example CRM integration", auth_type: "oauth" },
  { name: "test_analytics", display_name: "Test Analytics", description: "Example analytics integration", auth_type: "manual" },
  { name: "sample_storage", display_name: "Sample Storage", description: "Example storage integration", auth_type: "oauth" },
];

export async function GET() {
  const result = mockIntegrations.map((i) => ({
    ...i,
    connected: connectedIntegrations.has(i.name),
  }));
  return NextResponse.json(result);
}
