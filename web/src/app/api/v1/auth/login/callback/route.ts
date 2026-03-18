import { NextResponse } from "next/server";

export async function GET() {
  return NextResponse.json({
    email: "dev@toolshed.local",
    display_name: "Dev User",
    token: "mock-session-token",
  });
}
