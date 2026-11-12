import { PreviewShell } from "../preview-shell";

export default function AdminPreviewPage() {
  return (
    <PreviewShell activeView="admin">
      <section className="page-heading">
        <p className="eyebrow">ADMINISTRATION</p>
        <h1>Election management</h1>
        <p className="muted">Create elections, manage candidates, and control milestones.</p>
      </section>

      <section className="panel section-panel">
        <div className="section-heading">
          <div>
            <p className="eyebrow">SETUP</p>
            <h2>Create an election</h2>
          </div>
        </div>
        <form className="form-stack editor-form">
          <label>Election title<input placeholder="e.g. Student Council Election" disabled /></label>
          <label>Description<textarea rows={3} placeholder="What is this election for?" disabled /></label>
          <div className="date-grid date-input-grid">
            <label>Registration opens<input type="datetime-local" disabled /></label>
            <label>Registration closes<input type="datetime-local" disabled /></label>
            <label>Voting opens<input type="datetime-local" disabled /></label>
            <label>Voting closes<input type="datetime-local" disabled /></label>
          </div>
          <label>Initial status<select disabled defaultValue="draft"><option value="draft">Draft</option></select></label>
          <button className="button button-primary" disabled type="button">Create election</button>
        </form>
      </section>

      <section className="panel section-panel">
        <div className="section-heading">
          <div>
            <p className="eyebrow">CANDIDATES</p>
            <h2>Add a candidate</h2>
          </div>
        </div>
        <form className="form-stack candidate-form">
          <label>Election<select disabled defaultValue="sample"><option value="sample">Student Council Election 2026</option></select></label>
          <label>Candidate name<input placeholder="Candidate name" disabled /></label>
          <label>Description<textarea rows={2} placeholder="Short candidate introduction" disabled /></label>
          <button className="button button-primary" disabled type="button">Add candidate</button>
        </form>
        <p className="candidate-management-help">Candidates can be added, edited, or removed until voting starts.</p>
      </section>

      <section className="section-block">
        <div className="section-heading">
          <div>
            <p className="eyebrow">ELECTIONS</p>
            <h2>Manage elections</h2>
          </div>
        </div>
        <div className="election-list">
          <article className="panel election-card">
            <div className="election-card-heading">
              <div>
                <span className="status status-registration">registration</span>
                <h2>Student Council Election 2026</h2>
              </div>
            </div>
            <details className="edit-details">
              <summary>Edit election details</summary>
              <p className="muted">Election dates, description, and status would be editable here.</p>
            </details>
            <details className="edit-details candidate-management">
              <summary>Manage candidates (2)</summary>
              <div className="candidate-management-list">
                <div className="candidate-management-item">
                  <div className="candidate-edit-form">
                    <label>Candidate name<input defaultValue="Alex Morgan" disabled /></label>
                    <label>Description<textarea rows={2} defaultValue="Community and student events." disabled /></label>
                    <button className="button button-secondary" disabled type="button">Save candidate</button>
                  </div>
                </div>
                <div className="candidate-management-item">
                  <div className="candidate-edit-form">
                    <label>Candidate name<input defaultValue="Sam Rivera" disabled /></label>
                    <label>Description<textarea rows={2} defaultValue="Peer mentoring and collaboration." disabled /></label>
                    <button className="button button-secondary" disabled type="button">Save candidate</button>
                  </div>
                </div>
              </div>
              <p className="candidate-management-help">Candidate details can be changed until voting starts.</p>
            </details>
            <div className="milestone-form">
              <button className="button button-secondary" disabled type="button">Open voter registration</button>
              <label>Set status<select disabled defaultValue="registration"><option value="registration">Registration</option></select></label>
              <button className="button button-danger" disabled type="button">Delete election</button>
            </div>
          </article>
        </div>
      </section>
    </PreviewShell>
  );
}
