"use client";

import { useState } from "react";
import { obtainAnonymousCredential } from "@/lib/anonymous-credential";

export function CredentialForm({ electionId, userId }: { electionId: string; userId: string }) {
  const [busy, setBusy] = useState(false);
  const [saved, setSaved] = useState(false);
  const [error, setError] = useState("");

  async function issue() {
    setBusy(true);
    setError("");
    try {
      await obtainAnonymousCredential(electionId, userId);
      setSaved(true);
    } catch (error) {
      setError(error instanceof Error ? error.message : "Could not obtain the credential.");
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="form-stack">
      <p className="muted">Use the same browser to retry. Clearing browser data removes your credential.</p>
      <button className="button button-primary" type="button" disabled={busy || saved} onClick={issue}>
        {busy ? "Preparing credential…" : saved ? "Credential saved" : "Get anonymous credential"}
      </button>
      {saved && <p className="form-message" role="status">Your credential is saved in this browser.</p>}
      {error && <p className="form-message error-message" role="alert">{error}</p>}
    </div>
  );
}
