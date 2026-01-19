import { NextRequest, NextResponse } from "next/server";

const AIRBORNE_ADMIN_URL = process.env.AIRBORNE_ADMIN_URL || "http://localhost:8473";

export async function GET(
  request: NextRequest,
  { params }: { params: Promise<{ threadId: string }> }
) {
  const { threadId } = await params;

  if (!threadId) {
    return NextResponse.json({ error: "thread_id required" }, { status: 400 });
  }

  try {
    const response = await fetch(`${AIRBORNE_ADMIN_URL}/admin/threads/${threadId}`, {
      headers: {
        "Content-Type": "application/json",
      },
      cache: "no-store",
    });

    if (!response.ok) {
      if (response.status === 404) {
        return NextResponse.json({ error: "Thread not found", messages: [] }, { status: 200 });
      }
      return NextResponse.json(
        { error: `Airborne admin server returned status ${response.status}`, messages: [] },
        { status: 200 }
      );
    }

    const data = await response.json();
    return NextResponse.json(data);
  } catch (error) {
    const message = error instanceof Error ? error.message : "Unknown error";
    return NextResponse.json(
      { error: `Failed to connect to Airborne admin server: ${message}`, messages: [] },
      { status: 200 }
    );
  }
}
