"use client";

import { FormEvent, useState } from "react";
import { useRouter } from "next/navigation";

type ApiError = { error?: string };

async function requestError(response: Response) {
  const payload = (await response.json()) as ApiError;
  return payload.error || `Request failed (${response.status})`;
}

export function LoginForm() {
  const router = useRouter();
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [otp, setOtp] = useState("");
  const [otpRequired, setOtpRequired] = useState(false);
  const [busy, setBusy] = useState(false);
  const [message, setMessage] = useState("");

  async function startLogin(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setBusy(true);
    setMessage("");
    try {
      const response = await fetch("/api/login", {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({ email, password }),
      });
      if (!response.ok) throw new Error(await requestError(response));
      setOtpRequired(true);
      setMessage("Enter the six-digit code provided by your administrator.");
    } catch (error) {
      setMessage(error instanceof Error ? error.message : "Unable to sign in.");
    } finally {
      setBusy(false);
    }
  }

  async function verifyOtp(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setBusy(true);
    setMessage("");
    try {
      const response = await fetch("/api/login/verify-otp", {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({ otp }),
      });
      if (!response.ok) throw new Error(await requestError(response));
      const payload = (await response.json()) as {
        user: { role: "admin" | "voter" };
      };
      router.replace(payload.user.role === "admin" ? "/admin" : "/dashboard");
      router.refresh();
    } catch (error) {
      setMessage(error instanceof Error ? error.message : "Unable to verify code.");
    } finally {
      setBusy(false);
    }
  }

  async function resendOtp() {
    setBusy(true);
    setMessage("");
    try {
      const response = await fetch("/api/login/resend-otp", { method: "POST" });
      if (!response.ok) throw new Error(await requestError(response));
      setMessage("A new code has been requested.");
    } catch (error) {
      setMessage(error instanceof Error ? error.message : "Unable to resend code.");
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="form-stack">
      {!otpRequired ? (
        <form className="form-stack" onSubmit={startLogin}>
          <label>
            Email
            <input
              autoComplete="username"
              type="email"
              value={email}
              onChange={(event) => setEmail(event.target.value)}
              required
            />
          </label>
          <label>
            Password
            <input
              autoComplete="current-password"
              type="password"
              value={password}
              onChange={(event) => setPassword(event.target.value)}
              required
            />
          </label>
          <button className="button button-primary" disabled={busy} type="submit">
            {busy ? "Signing in…" : "Continue"}
          </button>
        </form>
      ) : (
        <form className="form-stack" onSubmit={verifyOtp}>
          <label>
            One-time code
            <input
              autoComplete="one-time-code"
              inputMode="numeric"
              pattern="[0-9]{6}"
              maxLength={6}
              value={otp}
              onChange={(event) =>
                setOtp(event.target.value.replace(/\D/g, "").slice(0, 6))
              }
              required
            />
          </label>
          <button className="button button-primary" disabled={busy} type="submit">
            {busy ? "Verifying…" : "Verify and sign in"}
          </button>
          <button
            className="button button-secondary"
            disabled={busy}
            onClick={resendOtp}
            type="button"
          >
            Resend code
          </button>
        </form>
      )}
      {message && <p className="form-message" role="status">{message}</p>}
    </div>
  );
}
