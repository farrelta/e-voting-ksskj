import Link from "next/link";
import { VerificationForm } from "./verification-form";

export default async function VerifyPage({
  searchParams,
}: {
  searchParams: Promise<{ receipt?: string }>;
}) {
  const { receipt } = await searchParams;
  return (
    <main className="auth-shell">
      <section className="auth-card verify-card">
        <p className="eyebrow">PUBLIC VERIFICATION</p>
        <h1>Verify a receipt</h1>
        <p className="muted">
          Check whether the receipt is linked to a recorded ballot and view its
          immutable ballot hash.
        </p>
        <VerificationForm initialReceipt={receipt ?? ""} />
        <Link className="text-link auth-link" href="/login">Return to sign in</Link>
      </section>
    </main>
  );
}
