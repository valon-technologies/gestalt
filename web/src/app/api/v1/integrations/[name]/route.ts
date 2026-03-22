import { NextResponse } from "next/server";
import { connectedIntegrations } from "../../_mock-state";

export async function DELETE(
  _request: Request,
  { params }: { params: Promise<{ name: string }> },
) {
  const { name } = await params;
  if (!connectedIntegrations.has(name)) {
    return NextResponse.json(
      { error: `no connection found for integration "${name}"` },
      { status: 404 },
    );
  }
  connectedIntegrations.delete(name);
  return NextResponse.json({ status: "disconnected" });
}
