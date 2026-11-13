"use client";

import { FormEvent, useState } from "react";

type Verification = {
  election_title: string;
  recorded: boolean;
  ballot_hash?: string;
};

export function VerificationForm() {
  const [hash, setHash] = useState("");
  const [result, setResult] = useState<Verification | null>(null);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);

  async function verify(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setBusy(true);
    setResult(null);
    setError("");
    try {
      const tracker = hash.trim();
      if (!/^[0-9a-f]{64}$/i.test(tracker)) {
        throw new Error("Enter the 64-character ballot hash shown after voting.");
      }
      const response = await fetch(`/api/anonymous/trackers/${encodeURIComponent(tracker)}`,
        { credentials: "omit" });
      const payload = (await response.json()) as Verification & { error?: string };
      if (!response.ok) throw new Error(payload.error || "Verification failed.");
      setResult(payload);
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : "Verification failed.");
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="form-stack">
      <form className="form-stack" onSubmit={verify}>
        <label>
          Ballot hash
          <input
            autoComplete="off"
            value={hash}
            onChange={(event) => setHash(event.target.value)}
            required
          />
        </label>
        <button className="button button-primary" disabled={busy} type="submit">
          {busy ? "Checking…" : "Verify ballot"}
        </button>
      </form>
      {error && <p className="form-message error-message" role="alert">{error}</p>}
      {result && (
        <section className="verification-result" aria-live="polite">
          <span className={result.recorded ? "result-mark success-mark" : "result-mark"}>
            {result.recorded ? "Ballot recorded" : "No ballot recorded"}
          </span>
          <p><strong>Election:</strong> {result.election_title}</p>
          {result.ballot_hash && (
            <p className="hash-row">
              <strong>Ballot hash</strong>
              <code>{result.ballot_hash}</code>
            </p>
          )}
        </section>
      )}
    </div>
  );
}
