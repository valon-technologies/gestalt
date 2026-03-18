import { NextResponse } from "next/server";
import { revokeMockToken } from "../../_mock-state";

export async function DELETE(_request: Request, { params }: { params: { id: string } }) {
  const found = revokeMockToken(params.id);
  if (!found) {
    return NextResponse.json({ error: "token not found" }, { status: 404 });
  }
  return NextResponse.json({ status: "revoked" });
}
