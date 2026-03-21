import { NextRequest, NextResponse } from "next/server";

export function middleware(request: NextRequest) {
  const backend = process.env.TOOLSHED_API_URL;
  if (!backend) return NextResponse.next();

  const url = new URL(request.url);
  const destination = `${backend}${url.pathname}${url.search}`;
  return NextResponse.rewrite(destination);
}

export const config = {
  matcher: "/api/v1/:path*",
};
