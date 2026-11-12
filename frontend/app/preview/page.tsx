import Link from "next/link";
import { PreviewShell } from "./preview-shell";

export default function PreviewPage() {
  return (
    <PreviewShell activeView={null}>
      <section className="page-heading">
        <p className="eyebrow">MOCKUP PREVIEW</p>
        <h1>Choose a view</h1>
        <p className="muted">
          Explore the voter dashboard and election administration screens using
          example content.
        </p>
      </section>
      <div className="preview-choice-grid">
        <Link className="panel preview-choice" href="/preview/dashboard">
          <p className="eyebrow">VOTER PORTAL</p>
          <h2>Dashboard</h2>
          <p className="muted">See election dates, registration status, and a sample ballot.</p>
          <span className="text-link">View voter dashboard →</span>
        </Link>
        <Link className="panel preview-choice" href="/preview/admin">
          <p className="eyebrow">ADMINISTRATION</p>
          <h2>Election management</h2>
          <p className="muted">See election setup, candidate management, and status controls.</p>
          <span className="text-link">View admin dashboard →</span>
        </Link>
      </div>
    </PreviewShell>
  );
}
