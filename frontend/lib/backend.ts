import { cookies } from "next/headers";

const backendUrl = process.env.BACKEND_URL ?? "http://localhost:3001";

export type SessionUser = {
  id: string;
  email: string;
  role: "admin" | "voter";
};

export type Election = {
  id: string;
  title: string;
  description: string;
  registration_start: string;
  registration_end: string;
  voting_start: string;
  voting_end: string;
  status: "draft" | "registration" | "active_voting" | "completed";
  is_registered?: boolean;
  has_voted?: boolean;
  receipt_id?: string;
  receipt_code?: string;
};

export type Candidate = {
  id: string;
  name: string;
  description: string;
};

export async function backendRequest(path: string, init: RequestInit = {}) {
  const cookieHeader = (await cookies()).toString();
  const headers = new Headers(init.headers);
  if (cookieHeader) headers.set("cookie", cookieHeader);
  if (init.body && !headers.has("content-type")) {
    headers.set("content-type", "application/json");
  }

  return fetch(`${backendUrl}${path}`, {
    ...init,
    headers,
    cache: "no-store",
  });
}

export async function readApiResponse<T>(response: Response): Promise<T> {
  const data = (await response.json()) as T & { error?: string };
  if (!response.ok) {
    throw new Error(data.error || `Request failed (${response.status})`);
  }
  return data;
}

export async function getElections() {
  const response = await backendRequest("/api/elections");
  return readApiResponse<{ elections: Election[] }>(response);
}

export async function getCandidates(electionId: string) {
  const response = await backendRequest(
    `/api/elections/${encodeURIComponent(electionId)}/candidates`,
  );
  return readApiResponse<{ candidates: Candidate[] }>(response);
}

export async function getSessionUser() {
  const response = await backendRequest("/api/me");
  return readApiResponse<{ user: SessionUser }>(response);
}
