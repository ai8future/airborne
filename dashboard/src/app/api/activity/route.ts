import { NextRequest, NextResponse } from "next/server";

const AIRBORNE_ADMIN_URL = process.env.AIRBORNE_ADMIN_URL || "http://localhost:50054";

export async function GET(request: NextRequest) {
  const searchParams = request.nextUrl.searchParams;
  const limit = searchParams.get("limit") || "50";
  const tenantId = searchParams.get("tenant_id");

  try {
    let url = `${AIRBORNE_ADMIN_URL}/admin/activity?limit=${limit}`;
    if (tenantId) {
      url += `&tenant_id=${encodeURIComponent(tenantId)}`;
    }

    const response = await fetch(url, {
      headers: {
        "Content-Type": "application/json",
      },
      // Don't cache the response - we want fresh data every poll
      cache: "no-store",
    });

    if (!response.ok) {
      return NextResponse.json(
        {
          activity: [],
          error: `Airborne admin server returned status ${response.status}`,
        },
        { status: 200 }
      );
    }

    const data = await response.json();
    return NextResponse.json(data);
  } catch (error) {
    const message = error instanceof Error ? error.message : "Unknown error";
    return NextResponse.json(
      {
        activity: [],
        error: `Failed to connect to Airborne admin server: ${message}`,
      },
      { status: 200 }
    );
  }
}
