import { NextRequest, NextResponse } from "next/server";

const AIRBORNE_ADMIN_URL = process.env.AIRBORNE_ADMIN_URL || "http://localhost:50054";

interface ChatRequest {
  thread_id: string;
  message: string;
  tenant_id?: string;
  provider?: string;
  system_prompt?: string;
}

interface ChatResponse {
  id?: string;
  content?: string;
  response?: string;
  provider?: string;
  model?: string;
  tokens_in?: number;
  tokens_out?: number;
  cost_usd?: number;
  error?: string;
}

export async function POST(request: NextRequest) {
  try {
    const body: ChatRequest = await request.json();

    if (!body.message || body.message.trim() === "") {
      return NextResponse.json(
        { error: "message is required" },
        { status: 400 }
      );
    }

    if (!body.thread_id) {
      return NextResponse.json(
        { error: "thread_id is required" },
        { status: 400 }
      );
    }

    // Try the chat endpoint first (with thread support)
    try {
      const chatResponse = await fetch(`${AIRBORNE_ADMIN_URL}/admin/chat`, {
        method: "POST",
        headers: {
          "Content-Type": "application/json",
        },
        body: JSON.stringify({
          thread_id: body.thread_id,
          message: body.message,
          tenant_id: body.tenant_id || "",
          provider: body.provider || "",
          system_prompt: body.system_prompt || "",
        }),
      });

      if (chatResponse.ok) {
        const data: ChatResponse = await chatResponse.json();
        return NextResponse.json(data);
      }

      // If chat endpoint doesn't exist (404), fall back to test endpoint
      if (chatResponse.status === 404) {
        const testResponse = await fetch(`${AIRBORNE_ADMIN_URL}/admin/test`, {
          method: "POST",
          headers: {
            "Content-Type": "application/json",
          },
          body: JSON.stringify({
            prompt: body.message,
            tenant_id: body.tenant_id || "",
            provider: body.provider || "gemini",
          }),
        });

        if (!testResponse.ok) {
          return NextResponse.json(
            { error: `Airborne admin server returned status ${testResponse.status}` },
            { status: testResponse.status }
          );
        }

        const testData = await testResponse.json();
        return NextResponse.json({
          id: `test-${Date.now()}`,
          content: testData.reply,
          provider: testData.provider,
          model: testData.model,
          tokens_in: testData.input_tokens,
          tokens_out: testData.output_tokens,
        });
      }

      return NextResponse.json(
        { error: `Airborne admin server returned status ${chatResponse.status}` },
        { status: chatResponse.status }
      );
    } catch (fetchError) {
      const message = fetchError instanceof Error ? fetchError.message : "Unknown error";
      return NextResponse.json(
        { error: `Failed to connect to Airborne admin server: ${message}` },
        { status: 500 }
      );
    }
  } catch (error) {
    const message = error instanceof Error ? error.message : "Unknown error";
    return NextResponse.json(
      { error: `Failed to process request: ${message}` },
      { status: 500 }
    );
  }
}
