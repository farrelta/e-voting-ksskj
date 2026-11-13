"use client";

import { type FormEvent, useState } from "react";
import Link from "next/link";
import { obtainAnonymousCredential } from "@/lib/anonymous-credential";
import { prepareAnonymousBallot } from "@/lib/anonymous-ballot";

type Manifest = {
  public_key: string;
  manifest_hash: string;
  candidates: { id: string; name: string }[];
};
type SavedBallot = { encrypted_vote: string; proof: string; token: string };

export function AnonymousBallotForm({ electionId, userId, closed }: {
  electionId: string; userId: string; closed: boolean;
}) {
  const [manifest, setManifest] = useState<Manifest>();
  const [choice, setChoice] = useState(-1);
  const [hasPending, setHasPending] = useState(false);
  const [tracker, setTracker] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const storageKey = `evoting:ballot:v1:${electionId}:${userId}`;
  const path = `/api/anonymous/elections/${encodeURIComponent(electionId)}`;

  async function openBallot() {
    setBusy(true);
    setError("");
    try {
      const saved = JSON.parse(localStorage.getItem(storageKey) ?? "{}") as {
        tracker?: string; ballot?: SavedBallot;
      };
      setHasPending(Boolean(saved.ballot));
      if (saved.tracker) {
        setTracker(saved.tracker);
        return;
      }
      if (closed && !saved.ballot) throw new Error("No saved ballot was found in this browser.");
      const response = await fetch(`${path}/manifest`, { credentials: "omit", cache: "no-store" });
      if (!response.ok) throw new Error("The ballot is unavailable.");
      setManifest(await response.json() as Manifest);
    } catch (error) {
      setError(error instanceof Error ? error.message : "Could not load the ballot.");
    } finally {
      setBusy(false);
    }
  }

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!manifest || busy || (!hasPending && choice < 0)) return;
    if (!navigator.locks) {
      setError("This browser cannot safely save ballot retries.");
      return;
    }
    setBusy(true);
    setError("");
    try {
      await navigator.locks.request(storageKey, async () => {
        const saved = JSON.parse(localStorage.getItem(storageKey) ?? "{}") as {
          tracker?: string; ballot?: SavedBallot;
        };
        if (saved.tracker) {
          setTracker(saved.tracker);
          return;
        }
        let ballot = saved.ballot;
        if (!ballot) {
          if (closed || choice < 0) throw new Error("Select a candidate while voting is open.");
          const token = await obtainAnonymousCredential(electionId, userId);
          const prepared = await prepareAnonymousBallot(electionId, manifest.public_key,
            manifest.manifest_hash, token, manifest.candidates.length, choice);
          ballot = { ...prepared, token };
          localStorage.setItem(storageKey, JSON.stringify({ ballot }));
          setHasPending(true);
        }
        const response = await fetch(`${path}/ballots`, {
          method: "POST", credentials: "omit", cache: "no-store", referrerPolicy: "no-referrer",
          headers: { "Content-Type": "application/json" }, body: JSON.stringify(ballot),
        });
        const result = await response.json() as { ballot_hash?: string; error?: string };
        if (!response.ok || !result.ballot_hash || !/^[0-9a-f]{64}$/.test(result.ballot_hash)) {
          throw new Error(result.error ?? "Could not confirm the ballot. Retry the saved ballot.");
        }
        localStorage.setItem(storageKey, JSON.stringify({ tracker: result.ballot_hash }));
        localStorage.removeItem(`evoting:credential:v1:${electionId}:${userId}`);
        setTracker(result.ballot_hash);
      });
    } catch (error) {
      setError(error instanceof Error ? error.message : "Could not confirm the ballot.");
    } finally {
      setBusy(false);
    }
  }

  if (tracker) return (
    <aside className="receipt-panel">
      <div>
        <span className="field-caption">Ballot recorded</span>
        <code>{tracker}</code>
        <span className="muted">Save this hash to verify your ballot later.</span>
      </div>
      <Link href="/verify" className="text-link">Verify ballot</Link>
    </aside>
  );

  return (
    <form className="form-stack" method="post" onSubmit={submit}>
      {!manifest ? (
        <button className="button button-secondary" type="button" onClick={openBallot} disabled={busy}>
          {busy ? "Loading ballot…" : closed ? "Check saved ballot" : "Open anonymous ballot"}
        </button>
      ) : (
        <>
          {!hasPending && (
            <fieldset className="candidate-options">
              <legend>Select one candidate</legend>
              {manifest.candidates.map((candidate, i) => (
                <label className="candidate-option" key={candidate.id}>
                  <input type="radio" checked={choice === i} onChange={() => setChoice(i)} disabled={busy} />
                  {candidate.name}
                </label>
              ))}
            </fieldset>
          )}
          {hasPending && <p className="muted">Retrying sends the same encrypted ballot.</p>}
          <button className="button button-primary" type="submit" disabled={busy || (!hasPending && choice < 0)}>
            {busy ? "Sending ballot…" : hasPending ? "Retry saved ballot" : "Submit anonymous ballot"}
          </button>
        </>
      )}
      {error && <p className="form-message error-message" role="alert">{error}</p>}
    </form>
  );
}
