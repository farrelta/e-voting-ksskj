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
  const [confirming, setConfirming] = useState(false);
  const [message, setMessage] = useState("");
  const [failed, setFailed] = useState(false);
  const [busy, setBusy] = useState(false);
  const cryptoSupported = isClientCryptoConfigured();
  const selected = candidates.find((candidate) => candidate.id === selectedCandidate);

  // Step 1: the voter picks a candidate and asks to continue.
  function review(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!selected || !cryptoSupported) return;
    setMessage("");
    setFailed(false);
    setConfirming(true);
  }

  async function castBallot() {
    if (!selected || busy) return;
    setBusy(true);
    setMessage("");
    setFailed(false);
    try {
      const payload = await prepareBallot({
        electionId,
        receiptId,
        candidateIndex: selected.index,
        candidateCount: candidates.length,
      });
      const result = await submitEncryptedBallot(
        electionId,
        receiptId,
        payload.encryptedVoteBase64,
        payload.proofBase64,
      );
      setMessage(result.message);
      setFailed(!result.ok);
      if (!result.ok) setConfirming(false);
    } catch (error) {
      setMessage(error instanceof Error ? error.message : "Ballot preparation failed.");
      setFailed(true);
      setConfirming(false);
    } finally {
      setBusy(false);
    }
  }

  if (candidates.length === 0) {
    return <p className="muted">There are no candidates available for this election.</p>;
  }

  if (confirming && selected) {
    return (
      <div className="form-stack ballot-form">
        <aside className="confirm-notice" role="alert">
          <span className="field-caption">You are about to vote for</span>
          <strong>{selected.name}</strong>
          <span>
            Once submitted, your ballot cannot be changed or viewed again. Your
            choice is encrypted in this browser before it is sent.
          </span>
        </aside>
        <div className="ballot-actions">
          <button
            className="button button-primary"
            disabled={busy}
            onClick={castBallot}
            type="button"
          >
            {busy ? "Encrypting and submitting…" : "Confirm and submit ballot"}
          </button>
          <button
            className="button button-secondary"
            disabled={busy}
            onClick={() => setConfirming(false)}
            type="button"
          >
            Go back
          </button>
        </div>
        {message && (
          <p className={failed ? "form-message error-message" : "form-message"} role="status">
            {message}
          </p>
        )}
      </div>
    );
  }

  return (
    <form className="form-stack ballot-form" onSubmit={review}>
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
      <aside className="privacy-notice" role="note">
        <strong>Your vote is encrypted before it leaves this page.</strong>
        <span>
          After you submit, this page will only show that you have voted. It will not
          show who you voted for, and no one else can see it either.
        </span>
      </aside>
      {!cryptoSupported && (
        <p className="form-message error-message" role="alert">
          This browser cannot run the encryption module, so voting is unavailable here.
        </p>
      )}
      <button
        className="button button-primary"
        disabled={!selected || !cryptoSupported}
        type="submit"
      >
        Review my choice
      </button>
      {message && (
        <p className={failed ? "form-message error-message" : "form-message"} role="status">
          {message}
        </p>
      )}
    </form>
  );
}
