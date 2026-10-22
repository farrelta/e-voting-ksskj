"use client";

import { FormEvent, useState } from "react";
import { submitEncryptedBallot } from "@/app/actions";
import { isClientCryptoConfigured, prepareBallot } from "@/lib/client-crypto";
import type { Candidate } from "@/lib/backend";

export function BallotForm({
  electionId,
  receiptId,
  candidates,
}: {
  electionId: string;
  receiptId: string;
  candidates: Candidate[];
}) {
  const [selectedCandidate, setSelectedCandidate] = useState("");
  const [message, setMessage] = useState("");
  const [busy, setBusy] = useState(false);
  const cryptoConfigured = isClientCryptoConfigured();

  async function vote(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!selectedCandidate || !cryptoConfigured) return;
    setBusy(true);
    setMessage("");
    try {
      const payload = await prepareBallot(selectedCandidate, electionId);
      const result = await submitEncryptedBallot(
        electionId,
        receiptId,
        payload.encryptedVoteBase64,
        payload.proofBase64,
      );
      setMessage(result.message);
    } catch (error) {
      setMessage(error instanceof Error ? error.message : "Ballot preparation failed.");
    } finally {
      setBusy(false);
    }
  }

  if (candidates.length === 0) {
    return <p className="muted">There are no candidates available for this election.</p>;
  }

  return (
    <form className="form-stack ballot-form" onSubmit={vote}>
      <fieldset className="candidate-options">
        <legend>Select a candidate</legend>
        {candidates.map((candidate) => (
          <label className="candidate-option" key={candidate.id}>
            <input
              type="radio"
              name={`candidate-${electionId}`}
              value={candidate.id}
              checked={selectedCandidate === candidate.id}
              onChange={() => setSelectedCandidate(candidate.id)}
            />
            <span>
              <strong>{candidate.name}</strong>
              {candidate.description && (
                <span className="muted candidate-description">{candidate.description}</span>
              )}
            </span>
          </label>
        ))}
      </fieldset>
      <aside className="security-notice" role="note">
        <strong>Secure ballot processing is not configured.</strong>
        <span>
          Selection remains in this browser. Submission is disabled until the audited
          encryption and zero-knowledge proof protocol is set up.
        </span>
      </aside>
      <button
        className="button button-primary"
        disabled={!selectedCandidate || !cryptoConfigured || busy}
        type="submit"
      >
        {busy ? "Preparing encrypted ballot…" : "Submit secure ballot"}
      </button>
      {message && <p className="form-message" role="status">{message}</p>}
    </form>
  );
}
