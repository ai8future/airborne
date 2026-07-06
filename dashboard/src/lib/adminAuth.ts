import { timingSafeEqual } from "crypto";
import { NextRequest, NextResponse } from "next/server";

const AUTH_COOKIE = "airborne_admin_token";

type CredentialSource = "authorization" | "x-api-key" | "cookie" | "none";

interface PresentedCredential {
  token: string;
  source: CredentialSource;
}

function dashboardAuthToken(): string {
  return process.env.DASHBOARD_ADMIN_TOKEN || process.env.AIRBORNE_ADMIN_TOKEN || "";
}

function backendAdminToken(): string {
  return process.env.AIRBORNE_ADMIN_TOKEN || process.env.DASHBOARD_ADMIN_TOKEN || "";
}

function extractPresentedCredential(request: NextRequest): PresentedCredential {
  const authorization = request.headers.get("authorization")?.trim() || "";
  if (authorization) {
    const lower = authorization.toLowerCase();
    if (lower.startsWith("bearer ")) {
      return { token: authorization.slice("bearer ".length).trim(), source: "authorization" };
    }
    return { token: authorization, source: "authorization" };
  }

  const apiKey = request.headers.get("x-api-key")?.trim();
  if (apiKey) {
    return { token: apiKey, source: "x-api-key" };
  }

  const cookieToken = request.cookies.get(AUTH_COOKIE)?.value?.trim();
  if (cookieToken) {
    return { token: cookieToken, source: "cookie" };
  }

  return { token: "", source: "none" };
}

function constantTimeEqual(got: string, want: string): boolean {
  if (!got || !want) {
    return false;
  }

  const gotBytes = Buffer.from(got);
  const wantBytes = Buffer.from(want);
  if (gotBytes.length !== wantBytes.length) {
    // Keep a timing-safe operation in the mismatch path so wrong-length probes
    // do not get a materially cheaper code path.
    timingSafeEqual(gotBytes, gotBytes);
    return false;
  }

  return timingSafeEqual(gotBytes, wantBytes);
}

export function requireDashboardAdmin(request: NextRequest): NextResponse | null {
  const expected = dashboardAuthToken();
  if (!expected) {
    return NextResponse.json(
      { error: "dashboard admin token is not configured" },
      { status: 503 }
    );
  }

  const credential = extractPresentedCredential(request);
  if (!constantTimeEqual(credential.token, expected)) {
    return NextResponse.json(
      { error: "unauthorized" },
      {
        status: 401,
        headers: {
          "WWW-Authenticate": 'Bearer realm="airborne-dashboard"',
        },
      }
    );
  }

  // Cookie-based dashboard auth is browser-friendly, but cookies are ambient
  // credentials. Require a same-origin Origin/Referer on state-changing calls
  // so a malicious site cannot drive the admin proxy via CSRF.
  if (credential.source === "cookie" && isStateChanging(request.method) && !isSameOrigin(request)) {
    return NextResponse.json(
      { error: "forbidden" },
      { status: 403 }
    );
  }

  return null;
}

export function adminFetchHeaders(headers: Record<string, string> = {}): Record<string, string> {
  const token = backendAdminToken();
  if (!token) {
    return headers;
  }
  return {
    ...headers,
    Authorization: `Bearer ${token}`,
  };
}

function isStateChanging(method: string): boolean {
  return !["GET", "HEAD", "OPTIONS"].includes(method.toUpperCase());
}

function isSameOrigin(request: NextRequest): boolean {
  const expectedOrigin = request.nextUrl.origin;
  const origin = request.headers.get("origin");
  if (origin) {
    return origin === expectedOrigin;
  }

  const referer = request.headers.get("referer");
  if (!referer) {
    return false;
  }

  try {
    return new URL(referer).origin === expectedOrigin;
  } catch {
    return false;
  }
}
