import type { ElectionResults } from "@/lib/backend";

function displayDateTime(value: string) {
  return new Intl.DateTimeFormat("en", {
    dateStyle: "medium",
    timeStyle: "short",
    timeZone: "Asia/Jakarta",
    timeZoneName: "short",
  }).format(new Date(value));
}

// Shows tallied counts. Candidates are listed by votes (highest first); ties keep
// the ballot order, and no winner is declared here.
export function ResultsTable({ results }: { results: ElectionResults }) {
  const counted = results.results.reduce((sum, item) => sum + item.votes, 0);
  const rows = results.results
    .map((item, position) => ({ ...item, position }))
    .sort((a, b) => b.votes - a.votes || a.position - b.position);

  return (
    <div className="results">
      <ul className="results-list">
        {rows.map((row) => {
          const share = counted > 0 ? (row.votes / counted) * 100 : 0;
          return (
            <li className="result-row" key={row.candidate_id}>
              <div className="result-label">
                <strong>{row.name}</strong>
                <span>
                  {row.votes} {row.votes === 1 ? "vote" : "votes"} · {share.toFixed(1)}%
                </span>
              </div>
              <div
                aria-hidden="true"
                className="result-bar"
              >
                <span style={{ width: `${share}%` }} />
              </div>
            </li>
          );
        })}
      </ul>
      <p className="muted results-meta">
        {counted} valid {counted === 1 ? "ballot" : "ballots"} counted · tallied{" "}
        {displayDateTime(results.tallied_at)}
      </p>
    </div>
  );
}
