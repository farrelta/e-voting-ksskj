import { NextRequest, NextResponse } from "next/server";

const backendUrl = process.env.BACKEND_URL ?? "http://localhost:3001";

function destinationFor(role: string) {
  return role === "admin" ? "/admin" : "/dashboard";
}

export async function middleware(request: NextRequest) {
  const { pathname } = request.nextUrl;
  const cookie = request.headers.get("cookie");

  if (!cookie) {
    return NextResponse.redirect(new URL("/login", request.url));
  }

  let response: Response;
  try {
    response = await fetch(`${backendUrl}/api/me`, {
      headers: { cookie },
      cache: "no-store",
    });
  } catch {
    return NextResponse.json(
      { error: "Authentication service is unavailable." },
      { status: 503 },
    );
  }

  if (response.status === 401) {
    return NextResponse.redirect(new URL("/login", request.url));
  }
  if (!response.ok) {
    return NextResponse.json(
      { error: "Unable to verify the current session." },
      { status: 503 },
    );
  }

  const payload = (await response.json()) as {
    user?: { role?: string };
  };
  const role = payload.user?.role;
  const allowed = pathname.startsWith("/admin")
    ? role === "admin"
    : role === "voter";

  if (!allowed) {
    return NextResponse.redirect(
      new URL(role ? destinationFor(role) : "/login", request.url),
    );
  }
  return NextResponse.next();
}

export const config = {
  matcher: ["/admin/:path*", "/dashboard/:path*"],
};
