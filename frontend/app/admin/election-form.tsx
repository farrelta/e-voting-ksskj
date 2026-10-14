"use client";

import { FormEvent, useEffect, useRef, useState, useTransition } from "react";
import { createElection, updateElection } from "@/app/actions";
import type { Election } from "@/lib/backend";

function localDateTime(value: string) {
  const date = new Date(value);
  const pad = (part: number) => String(part).padStart(2, "0");
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}T${pad(date.getHours())}:${pad(date.getMinutes())}`;
}

export function ElectionForm({ election }: { election?: Election }) {
  const formRef = useRef<HTMLFormElement>(null);
  const [pending, startTransition] = useTransition();
  const [error, setError] = useState("");
  const action = election ? updateElection : createElection;

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
  }, [election]);

  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setError("");
    const formData = new FormData(event.currentTarget);
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
        await action(formData);
      } catch (reason) {
        setError(reason instanceof Error ? reason.message : "Election save failed.");
      }
    });
  }

  return (
    <form className="form-stack editor-form" onSubmit={submit} ref={formRef}>
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
      <label>
        Initial status
        <select name="status" defaultValue={election?.status ?? "draft"}>
          <option value="draft">Draft</option>
          <option value="registration">Registration</option>
          <option value="active_voting">Active voting</option>
          <option value="completed">Completed</option>
        </select>
      </label>
      {error && <p className="form-message error-message" role="alert">{error}</p>}
      <button className="button button-primary" disabled={pending} type="submit">
        {pending ? "Saving…" : election ? "Save election" : "Create election"}
      </button>
    </form>
  );
}
