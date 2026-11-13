import Link from "next/link";
import { VerificationForm } from "./verification-form";

export default function VerifyPage() {
  return (
    <main className="auth-shell">
      <section className="auth-card verify-card">
        <p className="eyebrow">PUBLIC VERIFICATION</p>
        <h1>Verify a ballot</h1>
        <p className="muted">
          Enter your ballot hash to check whether your ballot was recorded.
        </p>
        <VerificationForm />
        <Link className="text-link auth-link" href="/login">Return to sign in</Link>
      </section>
    </main>
  );
}
