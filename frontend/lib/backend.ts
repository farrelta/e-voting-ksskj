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

  // Position of the candidate in the ordered ballot (0-based). The vote is encrypted as "slot <index>" and the tally reads the same slots back.
export type Candidate = {
  id: string;
  name: string;
  description: string;
  index: number;
};

export type CandidateResult = {
  candidate_id: string;
  name: string;
  votes: number;
};

export type ElectionResults = {
  election_id: string;
  total_ballots: number;
  results: CandidateResult[];
  tallied_at: string;
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

export async function getAdminCandidates(electionId: string) {
  const response = await backendRequest(
    `/api/admin/elections/${encodeURIComponent(electionId)}/candidates`,
  );
  return readApiResponse<{ candidates: Candidate[] }>(response);
}

export type VoterStatus = {
  user_id: string;
  email: string;
  has_voted: boolean;
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

// Public: results exist only after the admin has run the tally (404 before).
export async function getElectionResults(electionId: string) {
  const response = await backendRequest(
    `/api/elections/${encodeURIComponent(electionId)}/results`,
  );
  if (response.status === 404) return null;
  return readApiResponse<ElectionResults>(response);
}
