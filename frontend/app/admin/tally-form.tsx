"use client";

import { useState, useTransition, type FormEvent } from "react";
import { tallyElection } from "@/app/actions";
import { decryptTallyKey } from "./tally-key-backup";

export function TallyForm({ electionId }: { electionId: string }) {
  const [pending, startTransition] = useTransition();
  const [feedback, setFeedback] = useState<{ ok: boolean; message: string }>();

  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const form = event.currentTarget;
    const data = new FormData(form);
    const file = data.get("backup");
    const passphrase = data.get("passphrase");
    if (!(file instanceof File) || !file.size || file.size > 2_000_000 || typeof passphrase !== "string") {
      setFeedback({ ok: false, message: "Choose an encrypted tally key backup smaller than 2 MB." });
      return;
    }
    setFeedback(undefined);
    startTransition(async () => {
      try {
        const key = await decryptTallyKey(await file.text(), electionId, passphrase);
        const result = await tallyElection(electionId, key);
        setFeedback(result);
        if (result.ok) form.reset();
      } catch (error) {
        setFeedback({ ok: false, message: error instanceof Error ? error.message : "Tally failed." });
      }
    });
  }

  return (
    <form className="form-stack editor-form" method="post" onSubmit={submit}>
      <label>
        Encrypted tally key backup
        <input type="file" name="backup" accept=".json,application/json" required />
      </label>
      <label>
        Backup passphrase
        <input type="password" name="passphrase" required />
      </label>
      {feedback && (
        <p className={feedback.ok ? "form-message" : "form-message error-message"}
          role={feedback.ok ? "status" : "alert"}>{feedback.message}</p>
      )}
      <button className="button button-primary" type="submit" disabled={pending}>
        {pending ? "Counting votes…" : "Tally votes"}
      </button>
    </form>
  );
}
