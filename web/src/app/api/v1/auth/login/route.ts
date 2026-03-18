import { NextResponse } from "next/server";

export async function POST(request: Request) {
  const body = await request.json();
  const state = body.state || "";
  const callbackUrl = `/auth/callback?code=mock-code&state=${encodeURIComponent(state)}`;
  return NextResponse.json({ url: callbackUrl });
}
