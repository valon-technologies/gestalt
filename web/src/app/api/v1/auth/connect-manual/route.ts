import { NextResponse } from "next/server";
import { connectedIntegrations } from "../../_mock-state";

export async function POST(request: Request) {
  const body = await request.json();
  const { integration, credential } = body;
  if (!integration || !credential) {
    return NextResponse.json(
      { error: "integration and credential are required" },
      { status: 400 },
    );
  }
  connectedIntegrations.add(integration);
  return NextResponse.json({ status: "connected" });
}
