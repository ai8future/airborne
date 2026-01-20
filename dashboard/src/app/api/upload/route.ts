import { NextRequest, NextResponse } from "next/server";

const AIRBORNE_ADMIN_URL = process.env.AIRBORNE_ADMIN_URL || "http://localhost:50054";

interface UploadResponse {
  file_uri?: string;
  filename?: string;
  mime_type?: string;
  error?: string;
}

export async function POST(request: NextRequest) {
  try {
    const formData = await request.formData();
    const file = formData.get("file") as File | null;
    const tenantId = formData.get("tenant_id") as string | null;

    if (!file) {
      return NextResponse.json(
        { error: "file is required" },
        { status: 400 }
      );
    }

    // Forward the file to the backend upload endpoint
    const backendFormData = new FormData();
    backendFormData.append("file", file);
    if (tenantId) {
      backendFormData.append("tenant_id", tenantId);
    }

    const uploadResponse = await fetch(`${AIRBORNE_ADMIN_URL}/admin/upload`, {
      method: "POST",
      body: backendFormData,
    });

    if (!uploadResponse.ok) {
      const errorText = await uploadResponse.text();
      console.error("Upload failed:", errorText);
      return NextResponse.json(
        { error: `Upload failed: ${uploadResponse.status}` },
        { status: uploadResponse.status }
      );
    }

    const data: UploadResponse = await uploadResponse.json();

    if (data.error) {
      return NextResponse.json(
        { error: data.error },
        { status: 500 }
      );
    }

    return NextResponse.json(data);
  } catch (error) {
    const message = error instanceof Error ? error.message : "Unknown error";
    console.error("Upload error:", message);
    return NextResponse.json(
      { error: `Failed to upload file: ${message}` },
      { status: 500 }
    );
  }
}
