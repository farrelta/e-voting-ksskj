"use server";

import { revalidatePath } from "next/cache";
import { redirect } from "next/navigation";
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
