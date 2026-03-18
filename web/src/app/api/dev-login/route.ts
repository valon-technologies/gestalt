import { NextResponse } from "next/server";

export async function POST(request: Request) {
  const body = await request.json();
  const email = body.email || "dev@toolshed.local";

  const backend = process.env.TOOLSHED_API_URL;
  if (!backend) {
    return NextResponse.json({ email, token: "mock-dev-token" });
  }

  const res = await fetch(`${backend}/api/v1/tokens`, {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
      "X-Dev-User-Email": email,
    },
    body: JSON.stringify({ name: `dev-session-${Date.now()}` }),
  });

  if (!res.ok) {
    const text = await res.text();
    return NextResponse.json(
      { error: `Dev login failed: ${text}` },
      { status: res.status },
    );
  }

  const data = await res.json();
  return NextResponse.json({
    email,
    token: data.token,
  });
}
