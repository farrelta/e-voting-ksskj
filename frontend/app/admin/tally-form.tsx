"use client";

import { ChangeEvent, useState } from "react";
import { tallyElection } from "@/app/actions";
import { ConfirmSubmitButton } from "./confirm-submit-button";

// The secret key is large (hundreds of KB of base64), so it can be loaded from
// the file the admin saved at election creation instead of being pasted.
export function TallyForm({
  electionId,
  alreadyTallied,
}: {
  electionId: string;
  alreadyTallied: boolean;
}) {
  const [key, setKey] = useState("");
  const [fileName, setFileName] = useState("");

  async function loadFile(event: ChangeEvent<HTMLInputElement>) {
    const file = event.target.files?.[0];
    if (!file) return;
    setKey(await file.text());
    setFileName(file.name);
  }

  return (
    <form className="form-stack tally-form" action={tallyElection}>
      <input type="hidden" name="election_id" value={electionId} />
      <label>
        HE secret key
        <textarea
          autoComplete="off"
          name="he_secret_key"
          onChange={(event) => {
            setKey(event.target.value);
            setFileName("");
          }}
          placeholder="Paste the key, or load the file you saved when the election was created"
          rows={3}
          spellCheck={false}
          value={key}
        />
      </label>
      <label className="file-picker">
        Or load it from a file
        <input accept=".txt,.json,.key,text/plain,application/json" onChange={loadFile} type="file" />
      </label>
      {fileName && <p className="muted">Loaded {fileName}.</p>}
      <p className="candidate-management-help">
        The key is used once for this tally and is never stored by the server.
        Voting must have ended before votes can be counted.
      </p>
      <ConfirmSubmitButton
        className="button button-primary"
        message={
          alreadyTallied
            ? "Run the tally again? The saved results will be replaced."
            : "Count all ballots now? The election will be marked completed."
        }
      >
        {alreadyTallied ? "Re-run tally" : "Tally votes"}
      </ConfirmSubmitButton>
    </form>
  );
}
