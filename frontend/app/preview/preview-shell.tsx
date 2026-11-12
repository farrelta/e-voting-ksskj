import Link from "next/link";

export function PreviewShell({
  children,
  activeView,
}: {
  children: React.ReactNode;
  activeView: "admin" | "dashboard" | null;
}) {
  return (
    <main className="page-shell">
      <header className="topbar">
        <Link className="brand" href="/preview">E-Voting Preview</Link>
        <nav className="topbar-user" aria-label="Preview pages">
          <Link href="/preview/admin" aria-current={activeView === "admin" ? "page" : undefined}>
            Admin
          </Link>
          <Link href="/preview/dashboard" aria-current={activeView === "dashboard" ? "page" : undefined}>
            Dashboard
          </Link>
        </nav>
      </header>
      <p className="notice preview-notice">
        Preview only — sample data, no sign-in required, and actions are disabled.
      </p>
      {children}
    </main>
  );
}
