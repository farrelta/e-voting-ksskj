"use client";

import { FormEvent, useEffect, useRef, useState, useTransition } from "react";
import { useRouter } from "next/navigation";
import { createElection, updateElection } from "@/app/actions";
import type { Election } from "@/lib/backend";
import { encryptTallyKey } from "./tally-key-backup";

function localDateTime(value: string) {
  const date = new Date(value);
  const pad = (part: number) => String(part).padStart(2, "0");
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}T${pad(date.getHours())}:${pad(date.getMinutes())}`;
}

export function ElectionForm({ election }: { election?: Election }) {
  const formRef = useRef<HTMLFormElement>(null);
  const router = useRouter();
  const [pending, startTransition] = useTransition();
  const [error, setError] = useState("");
  const [success, setSuccess] = useState("");
  const [createdKey, setCreatedKey] = useState<{ id: string; he_secret_key: string }>();
  const [backup, setBackup] = useState("");

  useEffect(() => {
    if (!election || !formRef.current) return;
    for (const field of [
      "registration_start",
      "registration_end",
      "voting_start",
      "voting_end",
    ] as const) {
      const input = formRef.current.elements.namedItem(field);
      if (input instanceof HTMLInputElement) {
        input.value = localDateTime(election[field]);
      }
    }
  }, [
    election?.id,
    election?.registration_start,
    election?.registration_end,
    election?.voting_start,
    election?.voting_end,
  ]);

  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (pending || backup) return;
    setError("");
    setSuccess("");
    const formData = new FormData(event.currentTarget);
    const passphrase = formData.get("backup_passphrase");
    if (!election && (!window.crypto?.subtle || typeof passphrase !== "string" ||
        passphrase.length < 16 || passphrase !== formData.get("confirm_passphrase"))) {
      setError("Use HTTPS or localhost and enter matching backup passphrases of at least 16 characters.");
      return;
    }
    for (const field of [
      "registration_start",
      "registration_end",
      "voting_start",
      "voting_end",
    ]) {
      const value = formData.get(field);
      if (typeof value !== "string" || !value) {
        setError("All election dates are required.");
        return;
      }
      const date = new Date(value);
      if (Number.isNaN(date.getTime())) {
        setError("An election date is invalid.");
        return;
      }
      formData.set(field, date.toISOString());
    }
    startTransition(async () => {
      try {
        if (election) {
          const result = await updateElection(formData);
          if (!result.ok) {
            setError(result.message);
            return;
          }
        } else if (typeof passphrase === "string") {
          let key = createdKey;
          if (!key) {
            formData.delete("backup_passphrase");
            formData.delete("confirm_passphrase");
            const result = await createElection(formData);
            if (!result.ok) {
              setError(result.message);
              return;
            }
            if (!result.created?.id || !result.created.he_secret_key) {
              setError("The server did not return the tally key. Check the election before submitting again.");
              return;
            }
            key = result.created;
            setCreatedKey(key);
          }
          setBackup(await encryptTallyKey(key.id, key.he_secret_key, passphrase));
          setCreatedKey(undefined);
          formRef.current?.reset();
        }
        setSuccess(election ? "Election updated." : "Election created. Download your tally key backup.");
        router.refresh();
      } catch {
        setError(election ? "Election save failed." : "Could not finish setup. If the election was created, keep this page open and retry the backup.");
      }
    });
  }

  return (
    <form className="form-stack editor-form" method="post" onSubmit={submit} ref={formRef}>
      {election && <input type="hidden" name="election_id" value={election.id} />}
      <label>
        Election title
        <input name="title" defaultValue={election?.title ?? ""} required />
      </label>
      <label>
        Description
        <textarea name="description" rows={3} defaultValue={election?.description ?? ""} />
      </label>
      <div className="date-grid date-input-grid">
        <label>
          Registration opens
          <input
            name="registration_start"
            type="datetime-local"
            defaultValue={election?.registration_start.slice(0, 16) ?? ""}
            required
          />
        </label>
        <label>
          Registration closes
          <input
            name="registration_end"
            type="datetime-local"
            defaultValue={election?.registration_end.slice(0, 16) ?? ""}
            required
          />
        </label>
        <label>
          Voting opens
          <input
            name="voting_start"
            type="datetime-local"
            defaultValue={election?.voting_start.slice(0, 16) ?? ""}
            required
          />
        </label>
        <label>
          Voting closes
          <input
            name="voting_end"
            type="datetime-local"
            defaultValue={election?.voting_end.slice(0, 16) ?? ""}
            required
          />
        </label>
      </div>
      {election && <input type="hidden" name="status" value="draft" />}
      {!election && !backup && (
        <>
          <p className="form-message">Keep the backup and passphrase. You will need both to tally votes.</p>
          <label>
            Backup passphrase
            <input type="password" name="backup_passphrase" autoComplete="new-password" minLength={16} required />
          </label>
          <label>
            Confirm backup passphrase
            <input type="password" name="confirm_passphrase" autoComplete="new-password" minLength={16} required />
          </label>
        </>
      )}
      {error && <p className="form-message error-message" role="alert">{error}</p>}
      {success && <p className="form-message" role="status">{success}</p>}
      {backup ? (
        <>
          <a className="button button-primary" download="election-tally-key.json"
            href={`data:application/json;charset=utf-8,${encodeURIComponent(backup)}`}>
            Download encrypted tally key
          </a>
          <button className="button button-secondary" type="button" onClick={() => {
            setBackup("");
            setSuccess("");
          }}>
            I saved the backup
          </button>
        </>
      ) : (
        <button className="button button-primary" disabled={pending} type="submit">
          {pending ? "Saving…" : election ? "Save election" : createdKey ? "Retry key backup" : "Create election"}
        </button>
      )}
    </form>
  );
}
