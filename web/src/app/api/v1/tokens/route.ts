import { NextResponse } from "next/server";
import { mockTokens, createMockToken } from "../_mock-state";

export async function GET() {
  return NextResponse.json(mockTokens);
}

export async function POST(request: Request) {
  const body = await request.json();
  const { id, token } = createMockToken(body.name, body.scopes);
  return NextResponse.json({ id, name: body.name, token }, { status: 201 });
}
