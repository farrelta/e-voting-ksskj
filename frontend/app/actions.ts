"use server";

import { revalidatePath } from "next/cache";
import { headers } from "next/headers";
import { redirect } from "next/navigation";
import QRCode from "qrcode";
import { backendRequest, readApiResponse } from "@/lib/backend";

function formText(formData: FormData, key: string) {
  const value = formData.get(key);
  return typeof value === "string" ? value.trim() : "";
}

function isoDate(formData: FormData, key: string) {
  const value = formText(formData, key);
  const parsed = new Date(value);
  if (!value || Number.isNaN(parsed.getTime())) {
    throw new Error(`A valid ${key.replaceAll("_", " ")} date is required`);
  }
  return parsed.toISOString();
}

async function sendJson(path: string, method: string, body: unknown) {
  const response = await backendRequest(path, {
    method,
    body: JSON.stringify(body),
  });
  return readApiResponse<Record<string, unknown>>(response);
}

export async function createElection(formData: FormData) {
  try {
    await sendJson("/api/admin/elections", "POST", {
      title: formText(formData, "title"),
      description: formText(formData, "description"),
      registration_start: isoDate(formData, "registration_start"),
      registration_end: isoDate(formData, "registration_end"),
      voting_start: isoDate(formData, "voting_start"),
      voting_end: isoDate(formData, "voting_end"),
      status: formText(formData, "status"),
    });
  } catch (error) {
    const message = error instanceof Error ? error.message : "Election creation failed.";
    return { ok: false, message };
  }
  revalidatePath("/admin");
  return { ok: true, message: "Election created." };
}

export async function updateElection(formData: FormData) {
  const electionId = formText(formData, "election_id");
  try {
    await sendJson(`/api/admin/elections/${encodeURIComponent(electionId)}`, "PUT", {
      title: formText(formData, "title"),
      description: formText(formData, "description"),
      registration_start: isoDate(formData, "registration_start"),
      registration_end: isoDate(formData, "registration_end"),
      voting_start: isoDate(formData, "voting_start"),
      voting_end: isoDate(formData, "voting_end"),
      status: formText(formData, "status"),
    });
  } catch (error) {
    const message = error instanceof Error ? error.message : "Election update failed.";
    return { ok: false, message };
  }
  revalidatePath("/admin");
  revalidatePath("/dashboard");
  return { ok: true, message: "Election updated." };
}

export async function deleteElection(formData: FormData) {
  const electionId = formText(formData, "election_id");
  try {
    const response = await backendRequest(
      `/api/admin/elections/${encodeURIComponent(electionId)}`,
      { method: "DELETE" },
    );
    await readApiResponse(response);
  } catch (error) {
    const message = error instanceof Error ? error.message : "Election deletion failed.";
    redirect(`/admin?error=${encodeURIComponent(message)}`);
  }
  revalidatePath("/admin");
  revalidatePath("/dashboard");
  redirect("/admin?notice=Election%20deleted");
}

export async function updateElectionStatus(formData: FormData) {
  const electionId = formText(formData, "election_id");
  try {
    await sendJson(
      `/api/admin/elections/${encodeURIComponent(electionId)}/status`,
      "POST",
      { status: formText(formData, "status") },
    );
  } catch (error) {
    const message = error instanceof Error ? error.message : "Election status update failed.";
    redirect(`/admin?error=${encodeURIComponent(message)}`);
  }
  revalidatePath("/admin");
  revalidatePath("/dashboard");
  redirect("/admin?notice=Election%20status%20updated");
}

export async function createCandidate(formData: FormData) {
  try {
    await sendJson("/api/admin/candidates", "POST", {
      election_id: formText(formData, "election_id"),
      name: formText(formData, "name"),
      description: formText(formData, "description"),
    });
  } catch (error) {
    const message = error instanceof Error ? error.message : "Candidate creation failed.";
    redirect(`/admin?error=${encodeURIComponent(message)}`);
  }
  revalidatePath("/admin");
  redirect("/admin?notice=Candidate%20created");
}

export async function updateCandidate(formData: FormData) {
  const candidateId = formText(formData, "candidate_id");
  try {
    await sendJson(`/api/admin/candidates/${encodeURIComponent(candidateId)}`, "PUT", {
      name: formText(formData, "name"),
      description: formText(formData, "description"),
    });
  } catch (error) {
    const message = error instanceof Error ? error.message : "Candidate update failed.";
    redirect(`/admin?error=${encodeURIComponent(message)}`);
  }
  revalidatePath("/admin");
  revalidatePath("/dashboard");
  redirect("/admin?notice=Candidate%20updated");
}

export async function deleteCandidate(formData: FormData) {
  const candidateId = formText(formData, "candidate_id");
  try {
    const response = await backendRequest(
      `/api/admin/candidates/${encodeURIComponent(candidateId)}`,
      { method: "DELETE" },
    );
    await readApiResponse(response);
  } catch (error) {
    const message = error instanceof Error ? error.message : "Candidate deletion failed.";
    redirect(`/admin?error=${encodeURIComponent(message)}`);
  }
  revalidatePath("/admin");
  revalidatePath("/dashboard");
  redirect("/admin?notice=Candidate%20deleted");
}

export async function submitEncryptedBallot(
  electionId: string,
  receiptId: string,
  encryptedVote: string,
  proof: string,
) {
  if (!electionId || !receiptId || !encryptedVote || !proof) {
    return { ok: false, message: "Encrypted ballot data is incomplete." };
  }
  const response = await backendRequest(
    `/api/elections/${encodeURIComponent(electionId)}/ballots`,
    {
      method: "POST",
      body: JSON.stringify({
        receipt_id: receiptId,
        encrypted_vote: encryptedVote,
        proof,
      }),
    },
  );
  try {
    const result = await readApiResponse<{ ballot_hash: string }>(response);
    revalidatePath("/dashboard");
    return {
      ok: true,
      message: `Ballot recorded. Hash: ${result.ballot_hash}`,
    };
  } catch (error) {
    return {
      ok: false,
      message: error instanceof Error ? error.message : "Ballot submission failed.",
    };
  }
}

type BulkResult = {
  added: { email: string }[];
  already_eligible: string[];
  skipped: { email: string; reason: string }[];
  invalid: string[];
  allowed_domain: string;
};

function sample(items: string[], max = 3) {
  const shown = items.slice(0, max).join(", ");
  return items.length > max ? `${shown}, and ${items.length - max} more` : shown;
}

// Adds every address found in the pasted text (any mix of lines, commas and
// spaces; names and other columns are ignored). Only addresses on the allowed
// campus domain are accepted; the rest are reported back.
export async function addParticipants(formData: FormData) {
  const electionId = formText(formData, "election_id");
  let result: BulkResult;
  try {
    const response = await backendRequest(
      `/api/admin/elections/${encodeURIComponent(electionId)}/voters/bulk`,
      { method: "POST", body: JSON.stringify({ text: formData.get("emails") ?? "" }) },
    );
    result = await readApiResponse<BulkResult>(response);
  } catch (error) {
    const message = error instanceof Error ? error.message : "Could not add participants.";
    redirect(`/admin?error=${encodeURIComponent(message)}`);
  }

  const parts = [`${result.added.length} added`];
  if (result.already_eligible.length > 0) {
    parts.push(`${result.already_eligible.length} already on the list`);
  }
  if (result.invalid.length > 0) {
    parts.push(
      `${result.invalid.length} not added because they are not @${result.allowed_domain} addresses (${sample(result.invalid)})`,
    );
  }
  if (result.skipped.length > 0) {
    parts.push(
      `${result.skipped.length} skipped (${sample(result.skipped.map((item) => `${item.email}: ${item.reason}`), 2)})`,
    );
  }
  const problems = result.invalid.length + result.skipped.length;
  const key = result.added.length === 0 && problems > 0 ? "error" : "notice";
  revalidatePath("/admin");
  redirect(`/admin?${key}=${encodeURIComponent(parts.join(". ") + ".")}`);
}

export async function revokeEligibility(formData: FormData) {
  const electionId = formText(formData, "election_id");
  const userId = formText(formData, "user_id");
  try {
    const response = await backendRequest(
      `/api/admin/elections/${encodeURIComponent(electionId)}/voters/${encodeURIComponent(userId)}`,
      { method: "DELETE" },
    );
    await readApiResponse(response);
  } catch (error) {
    const message = error instanceof Error ? error.message : "Could not remove voter.";
    redirect(`/admin?error=${encodeURIComponent(message)}`);
  }
  revalidatePath("/admin");
  redirect("/admin?notice=Voter%20removed%20from%20the%20eligible%20list");
}

async function publicOrigin() {
  const configured = process.env.PUBLIC_APP_URL?.trim().replace(/\/+$/, "");
  if (configured) return configured;
  const h = await headers();
  const host = h.get("x-forwarded-host") ?? h.get("host") ?? "localhost:3000";
  const proto = h.get("x-forwarded-proto") ?? "http";
  return `${proto}://${host}`;
}

export type VoterQrResult =
  | {
      ok: true;
      qrDataUrl: string;
      url: string;
      expiresInSeconds: number;
      unreachableFromPhone: boolean;
    }
  | { ok: false; message: string };

// Issues a single-use login QR for one eligible voter. The token only appears in
// the QR/link and is never stored by the frontend.
export async function createVoterQr(
  electionId: string,
  userId: string,
): Promise<VoterQrResult> {
  try {
    const response = await backendRequest(
      `/api/admin/elections/${encodeURIComponent(electionId)}/voters/${encodeURIComponent(userId)}/qr`,
      { method: "POST" },
    );
    const data = await readApiResponse<{
      token: string;
      expires_in_seconds: number;
    }>(response);
    const url = `${await publicOrigin()}/scan?token=${encodeURIComponent(data.token)}`;
    const qrDataUrl = await QRCode.toDataURL(url, { margin: 2, width: 320 });
    const host = new URL(url).hostname;
    return {
      ok: true,
      qrDataUrl,
      url,
      expiresInSeconds: data.expires_in_seconds,
      unreachableFromPhone: host === "localhost" || host === "127.0.0.1",
    };
  } catch (error) {
    return {
      ok: false,
      message: error instanceof Error ? error.message : "Could not create QR code.",
    };
  }
}
