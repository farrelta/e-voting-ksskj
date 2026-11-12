import Link from "next/link";
import { ResultsTable } from "@/app/components/results-table";
import { getElectionResults } from "@/lib/backend";

const uuidPattern = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

// Public page: anyone with the link can see tallied results. Nothing here can
// show an individual voter or how they voted.
export default async function ResultsPage({
  params,
}: {
  params: Promise<{ electionId: string }>;
}) {
  const { electionId } = await params;
  const results = uuidPattern.test(electionId)
    ? await getElectionResults(electionId)
    : null;

  return (
    <main className="auth-shell">
      <section className="auth-card verify-card">
        <p className="eyebrow">ELECTION RESULTS</p>
        <h1>Results</h1>
        {results ? (
          <ResultsTable results={results} />
        ) : (
          <p className="muted">
            Results are not available yet. They are published after voting ends and
            the votes have been counted.
          </p>
        )}
        <Link className="text-link auth-link" href="/login">Return to sign in</Link>
      </section>
    </main>
  );
}
