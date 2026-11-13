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
  has_issuer: boolean;
  recorded_ballots?: number;
  is_registered?: boolean;
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
  return readApiResponse<{ elections: Election[]; ballot_pilot: boolean }>(response);
}

export async function getAdminCandidates(electionId: string) {
  const response = await backendRequest(
    `/api/admin/elections/${encodeURIComponent(electionId)}/candidates`,
  );
  return readApiResponse<{ candidates: Candidate[] }>(response);
}

export type VoterStatus = {
  credential_issued: boolean;
  user_id: string;
  email: string;
};

export async function getVoters(electionId: string) {
  const response = await backendRequest(
    `/api/admin/elections/${encodeURIComponent(electionId)}/voters`,
  );
  return readApiResponse<{ voters: VoterStatus[] }>(response);
}

export async function getSessionUser() {
  const response = await backendRequest("/api/me");
  return readApiResponse<{ user: SessionUser }>(response);
}
