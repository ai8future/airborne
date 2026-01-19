import { NextRequest, NextResponse } from "next/server";

const AIRBORNE_ADMIN_URL = process.env.AIRBORNE_ADMIN_URL || "http://localhost:50054";

export async function GET(
  request: NextRequest,
  { params }: { params: Promise<{ id: string }> }
) {
  const { id } = await params;

  if (!id) {
    return NextResponse.json({ error: "message_id required" }, { status: 400 });
  }

  try {
    const response = await fetch(`${AIRBORNE_ADMIN_URL}/admin/debug/${id}`, {
      headers: {
        "Content-Type": "application/json",
      },
      cache: "no-store",
    });

    if (!response.ok) {
      if (response.status === 404) {
        return NextResponse.json({ error: "Debug data not found" }, { status: 404 });
      }
      return NextResponse.json(
        { error: `Airborne admin server returned status ${response.status}` },
        { status: response.status }
      );
    }

    const data = await response.json();
    return NextResponse.json(data);
  } catch (error) {
    const message = error instanceof Error ? error.message : "Unknown error";
    return NextResponse.json(
      { error: `Failed to connect to Airborne admin server: ${message}` },
      { status: 503 }
    );
  }
}
