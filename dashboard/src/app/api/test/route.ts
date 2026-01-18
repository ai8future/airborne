import { NextRequest, NextResponse } from "next/server";

const AIRBORNE_ADMIN_URL = process.env.AIRBORNE_ADMIN_URL || "http://localhost:50054";

interface TestRequest {
  prompt: string;
  tenant_id?: string;
  provider?: string;
}

interface TestResponse {
  reply: string;
  provider: string;
  model: string;
  input_tokens: number;
  output_tokens: number;
  processing_ms: number;
  error?: string;
}

export async function POST(request: NextRequest) {
  try {
    const body: TestRequest = await request.json();

    if (!body.prompt || body.prompt.trim() === "") {
      return NextResponse.json(
        { error: "prompt is required" },
        { status: 400 }
      );
    }

    const response = await fetch(`${AIRBORNE_ADMIN_URL}/admin/test`, {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
      },
      body: JSON.stringify({
        prompt: body.prompt,
        tenant_id: body.tenant_id || "",
        provider: body.provider || "gemini",
      }),
    });

    if (!response.ok) {
      return NextResponse.json(
        { error: `Airborne admin server returned status ${response.status}` },
        { status: response.status }
      );
    }

    const data: TestResponse = await response.json();
    return NextResponse.json(data);
  } catch (error) {
    const message = error instanceof Error ? error.message : "Unknown error";
    return NextResponse.json(
      { error: `Failed to connect to Airborne admin server: ${message}` },
      { status: 500 }
    );
  }
}
