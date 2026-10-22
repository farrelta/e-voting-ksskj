import { LoginForm } from "./login-form";

export default function LoginPage() {
  return (
    <main className="auth-shell">
      <section className="auth-card">
        <p className="eyebrow">SECURE ELECTIONS</p>
        <h1>Welcome back</h1>
        <p className="muted">Sign in to continue to your voting portal.</p>
        <LoginForm />
        <a className="text-link auth-link" href="/verify">
          Verify a ballot receipt
        </a>
      </section>
    </main>
  );
}
