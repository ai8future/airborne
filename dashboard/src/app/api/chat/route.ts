import { NextRequest, NextResponse } from "next/server";

const AIRBORNE_ADMIN_URL = process.env.AIRBORNE_ADMIN_URL || "http://localhost:50054";
const MAX_RETRIES = 3;
const RETRY_DELAY_MS = 1000;

interface ChatRequest {
  thread_id: string;
  message: string;
  tenant_id?: string;
  provider?: string;
  system_prompt?: string;
  file_uri?: string;      // File URI from upload endpoint
  file_mime_type?: string; // MIME type of the file
  filename?: string;       // Original filename
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

// Retry fetch with exponential backoff
async function fetchWithRetry(
  url: string,
  options: RequestInit,
  retries: number = MAX_RETRIES
): Promise<Response> {
  let lastError: Error | null = null;

  for (let attempt = 0; attempt < retries; attempt++) {
    try {
      const response = await fetch(url, options);
      return response;
    } catch (error) {
      lastError = error instanceof Error ? error : new Error(String(error));
      console.warn(`Fetch attempt ${attempt + 1}/${retries} failed:`, lastError.message);

      if (attempt < retries - 1) {
        // Exponential backoff: 1s, 2s, 4s...
        const delay = RETRY_DELAY_MS * Math.pow(2, attempt);
        await new Promise(resolve => setTimeout(resolve, delay));
      }
    }
  }

  throw lastError || new Error("Fetch failed after retries");
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

    // Try the chat endpoint with retry logic
    try {
      const chatResponse = await fetchWithRetry(
        `${AIRBORNE_ADMIN_URL}/admin/chat`,
        {
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
            file_uri: body.file_uri || "",
            file_mime_type: body.file_mime_type || "",
            filename: body.filename || "",
          }),
        }
      );

      if (chatResponse.ok) {
        const data: ChatResponse = await chatResponse.json();
        return NextResponse.json(data);
      }

      // If chat endpoint doesn't exist (404), fall back to test endpoint
      if (chatResponse.status === 404) {
        const testResponse = await fetchWithRetry(
          `${AIRBORNE_ADMIN_URL}/admin/test`,
          {
            method: "POST",
            headers: {
              "Content-Type": "application/json",
            },
            body: JSON.stringify({
              prompt: body.message,
              tenant_id: body.tenant_id || "",
              provider: body.provider || "gemini",
            }),
          }
        );

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
        { error: `Failed to connect to Airborne admin server after ${MAX_RETRIES} attempts: ${message}` },
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
