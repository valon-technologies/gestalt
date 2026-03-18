import { NextResponse } from "next/server";
import { connectedIntegrations } from "../../_mock-state";

export async function POST(request: Request) {
  const body = await request.json();
  const integration = body.integration || "unknown";
  connectedIntegrations.add(integration);
  return NextResponse.json({
    url: "/integrations",
    state: "mock-state",
  });
}
