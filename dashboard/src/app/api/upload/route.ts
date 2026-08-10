import { NextRequest, NextResponse } from "next/server";
import { adminFetchHeaders, requireDashboardAdmin } from "@/lib/adminAuth";

const AIRBORNE_ADMIN_URL = process.env.AIRBORNE_ADMIN_URL || "http://localhost:50054";
const MAX_UPLOAD_BYTES = 100 * 1024 * 1024;

interface UploadResponse {
  file_uri?: string;
  filename?: string;
  mime_type?: string;
  error?: string;
}

export async function POST(request: NextRequest) {
  const authError = requireDashboardAdmin(request);
  if (authError) return authError;

  try {
    const contentLength = Number.parseInt(request.headers.get("content-length") || "0", 10);
    if (contentLength > MAX_UPLOAD_BYTES) {
      return NextResponse.json(
        { error: `file exceeds maximum upload size of ${MAX_UPLOAD_BYTES} bytes` },
        { status: 413 }
      );
    }

    const formData = await request.formData();
    const file = formData.get("file") as File | null;
    const tenantId = formData.get("tenant_id") as string | null;

    if (!file) {
      return NextResponse.json(
        { error: "file is required" },
        { status: 400 }
      );
    }
    if (file.size > MAX_UPLOAD_BYTES) {
      return NextResponse.json(
        { error: `file exceeds maximum upload size of ${MAX_UPLOAD_BYTES} bytes` },
        { status: 413 }
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
      headers: adminFetchHeaders(),
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
