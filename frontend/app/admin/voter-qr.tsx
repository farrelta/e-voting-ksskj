"use client";

import { useState, useTransition } from "react";
import { createVoterQr } from "@/app/actions";
import type { VoterQrResult } from "@/app/actions";

export function VoterQr({
  electionId,
  userId,
  email,
}: {
  electionId: string;
  userId: string;
  email: string;
}) {
  const [result, setResult] = useState<VoterQrResult | null>(null);
  const [pending, startTransition] = useTransition();

  function generate() {
    startTransition(async () => {
      setResult(await createVoterQr(electionId, userId));
    });
  }

  return (
    <div className="voter-qr">
      <button
        className="button button-secondary"
        disabled={pending}
        onClick={generate}
        type="button"
      >
        {pending ? "Creating…" : result?.ok ? "New QR" : "Login QR"}
      </button>
      {result && !result.ok && (
        <p className="form-message error-message" role="alert">{result.message}</p>
      )}
      {result?.ok && (
        <div className="qr-box">
          {/* eslint-disable-next-line @next/next/no-img-element */}
          <img src={result.qrDataUrl} alt={`Login QR code for ${email}`} width={240} height={240} />
          <p className="muted">
            For <strong>{email}</strong>. Single use, expires in{" "}
            {Math.round(result.expiresInSeconds / 60)} minutes. A new QR cancels this one.
          </p>
          {result.unreachableFromPhone && (
            <p className="form-message error-message">
              This link points to localhost, which a phone cannot open. Set
              PUBLIC_APP_URL to an address the phone can reach.
            </p>
          )}
          <button
            className="button button-secondary"
            onClick={() => setResult(null)}
            type="button"
          >
            Hide QR
          </button>
        </div>
      )}
    </div>
  );
}
