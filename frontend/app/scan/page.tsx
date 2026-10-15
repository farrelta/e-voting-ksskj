import { LoginForm } from "../login/login-form";

export default async function ScanPage({
  searchParams,
}: {
  searchParams: Promise<{ token?: string }>;
}) {
  const { token } = await searchParams;
  return (
    <main className="auth-shell">
      <section className="auth-card">
        <p className="eyebrow">SECURE ELECTIONS</p>
        <h1>Voter sign-in</h1>
        {token ? (
          <>
            <p className="muted">Your QR code identifies you. One more step to confirm it is you.</p>
            <LoginForm qrToken={token} />
          </>
        ) : (
          <>
            <p className="muted">
              This page opens when you scan the QR code from your election administrator.
            </p>
            <a className="text-link auth-link" href="/login">Go to sign in</a>
          </>
        )}
      </section>
    </main>
  );
}
