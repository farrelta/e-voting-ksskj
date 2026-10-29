package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const maxParticipantsPerRequest = 2000

// Accounts created from an admin's participant list have no password. This
// marker is not a valid bcrypt hash, so password login can never succeed for
// them; the intended way in for these accounts is the campus SSO.
const lockedPasswordHash = "!"

var (
	errElectionMissing = errors.New("election not found")
	errElectionClosed  = errors.New("election is completed")
)

// Finds email-looking text anywhere in a pasted block. Names, headers and other
// columns of a copied spreadsheet are ignored.
var emailPattern = regexp.MustCompile(`[A-Za-z0-9._%+\-]+@[A-Za-z0-9\-]+(?:\.[A-Za-z0-9\-]+)+`)

// extractEmails returns every distinct email address in text, lower-cased, in
// the order first seen.
func extractEmails(text string) []string {
	seen := make(map[string]struct{})
	var out []string
	for _, match := range emailPattern.FindAllString(text, -1) {
		email := strings.ToLower(match)
		if _, dup := seen[email]; dup {
			continue
		}
		seen[email] = struct{}{}
		out = append(out, email)
	}
	return out
}

// emailInDomain reports whether email is on domain or one of its subdomains
// ("mail.ugm.ac.id" is accepted for "ugm.ac.id"; "evilugm.ac.id" is not).
func emailInDomain(email, domain string) bool {
	at := strings.LastIndex(email, "@")
	if at < 1 || len(email) > 254 || domain == "" {
		return false
	}
	host := email[at+1:]
	return host == domain || strings.HasSuffix(host, "."+domain)
}

type addedParticipant struct {
	Email  string `json:"email"`
	UserID string `json:"user_id"`
}

type skippedParticipant struct {
	Email  string `json:"email"`
	Reason string `json:"reason"`
}

type participantSummary struct {
	Added           []addedParticipant   `json:"added"`
	AlreadyEligible []string             `json:"already_eligible"`
	Skipped         []skippedParticipant `json:"skipped"`
}

type accountInfo struct {
	id       string
	role     string
	verified bool
}

// addParticipants makes every email in emails (already lower-cased and domain
// checked) eligible for the election, creating a password-less voter account
// for anyone who has none yet. It runs in one transaction using set-based SQL,
// so a few thousand emails take a handful of queries. The admin learns only who
// is on the list, never anything about votes.
func (a *App) addParticipants(ctx context.Context, electionID string, emails []string) (participantSummary, error) {
	sum := participantSummary{
		Added:           []addedParticipant{},
		AlreadyEligible: []string{},
		Skipped:         []skippedParticipant{},
	}
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return sum, err
	}
	defer tx.Rollback(ctx)

	// Locking the election serialises concurrent changes to its voter list.
	var status string
	err = tx.QueryRow(ctx,
		`SELECT status FROM evoting.elections WHERE id = $1::uuid FOR UPDATE`,
		electionID,
	).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return sum, errElectionMissing
	}
	if err != nil {
		return sum, err
	}
	if status == "completed" {
		return sum, errElectionClosed
	}

	// 1. Accounts that already exist.
	accounts := make(map[string]accountInfo, len(emails))
	rows, err := tx.Query(ctx, `
		SELECT id::text, lower(email), role::text, is_verified
		FROM evoting.users WHERE lower(email) = ANY($1::text[])`, emails)
	if err != nil {
		return sum, err
	}
	for rows.Next() {
		var id, email, role string
		var verified bool
		if err := rows.Scan(&id, &email, &role, &verified); err != nil {
			rows.Close()
			return sum, err
		}
		accounts[email] = accountInfo{id: id, role: role, verified: verified}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return sum, err
	}

	// 2. Create voter accounts for the rest.
	var missing []string
	for _, email := range emails {
		if _, ok := accounts[email]; !ok {
			missing = append(missing, email)
		}
	}
	if len(missing) > 0 {
		rows, err := tx.Query(ctx, `
			INSERT INTO evoting.users (email, password_hash, role, is_verified)
			SELECT e, $2::text, 'voter'::evoting.roles, TRUE FROM unnest($1::text[]) AS e
			ON CONFLICT (email) DO NOTHING
			RETURNING id::text, email`, missing, lockedPasswordHash)
		if err != nil {
			return sum, err
		}
		for rows.Next() {
			var id, email string
			if err := rows.Scan(&id, &email); err != nil {
				rows.Close()
				return sum, err
			}
			accounts[email] = accountInfo{id: id, role: "voter", verified: true}
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return sum, err
		}
		for _, email := range missing {
			if _, ok := accounts[email]; !ok {
				return sum, fmt.Errorf("account for %s was created concurrently; retry", email)
			}
		}
	}

	// 3. Only verified voter accounts can be participants.
	var candidates, candidateIDs []string
	for _, email := range emails {
		acct := accounts[email]
		switch {
		case acct.role != "voter":
			sum.Skipped = append(sum.Skipped, skippedParticipant{email, "this email belongs to a non-voter account"})
		case !acct.verified:
			sum.Skipped = append(sum.Skipped, skippedParticipant{email, "this account is not verified"})
		default:
			candidates = append(candidates, email)
			candidateIDs = append(candidateIDs, acct.id)
		}
	}

	// 4. Who is already on the list?
	alreadyEligible := make(map[string]bool)
	if len(candidateIDs) > 0 {
		rows, err := tx.Query(ctx, `
			SELECT user_id::text FROM evoting.voter_eligibility
			WHERE election_id = $1::uuid AND user_id = ANY($2::text[]::uuid[])`,
			electionID, candidateIDs)
		if err != nil {
			return sum, err
		}
		for rows.Next() {
			var userID string
			if err := rows.Scan(&userID); err != nil {
				rows.Close()
				return sum, err
			}
			alreadyEligible[userID] = true
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return sum, err
		}
	}

	// 5. Issue a receipt and an eligibility row to everyone new.
	var userIDs, receiptIDs, codes []string
	for _, email := range candidates {
		acct := accounts[email]
		if alreadyEligible[acct.id] {
			sum.AlreadyEligible = append(sum.AlreadyEligible, email)
			continue
		}
		code, err := newReceiptCode()
		if err != nil {
			return sum, err
		}
		userIDs = append(userIDs, acct.id)
		receiptIDs = append(receiptIDs, uuid.NewString())
		codes = append(codes, code)
		sum.Added = append(sum.Added, addedParticipant{Email: email, UserID: acct.id})
	}
	if len(userIDs) > 0 {
		if _, err := tx.Exec(ctx, `
			INSERT INTO evoting.receipts (id, election_id, receipt_code)
			SELECT r::uuid, $1::uuid, c FROM unnest($2::text[], $3::text[]) AS t(r, c)`,
			electionID, receiptIDs, codes); err != nil {
			return sum, err
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO evoting.voter_eligibility (election_id, user_id, receipt_id)
			SELECT $1::uuid, u::uuid, r::uuid FROM unnest($2::text[], $3::text[]) AS t(u, r)`,
			electionID, userIDs, receiptIDs); err != nil {
			return sum, err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return sum, err
	}
	return sum, nil
}

func writeParticipantError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, errElectionMissing):
		writeError(w, http.StatusNotFound, "Election not found")
	case errors.Is(err, errElectionClosed):
		writeError(w, http.StatusConflict, "Election is completed; eligibility can no longer change")
	default:
		log.Printf("add participants: %v", err)
		writeError(w, http.StatusInternalServerError, "Failed to update the participant list")
	}
}

// POST /api/admin/elections/{electionId}/voters/bulk
// Body: {"text": "<pasted emails, any separators>"}
//
// Adds every @<allowed domain> address found in the text. Other addresses are
// reported back as invalid instead of being added.
func (a *App) handleBulkAddParticipants(w http.ResponseWriter, r *http.Request) {
	electionID := r.PathValue("electionId")
	if _, err := uuid.Parse(electionID); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid election ID")
		return
	}
	var body struct {
		Text string `json:"text"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	found := extractEmails(body.Text)
	if len(found) == 0 {
		writeError(w, http.StatusBadRequest, "No email addresses were found in the list")
		return
	}
	if len(found) > maxParticipantsPerRequest {
		writeError(w, http.StatusBadRequest,
			fmt.Sprintf("Too many addresses; add at most %d at a time", maxParticipantsPerRequest))
		return
	}

	domain := a.cfg.AllowedEmailDomain
	valid := []string{}
	invalid := []string{}
	for _, email := range found {
		if emailInDomain(email, domain) {
			valid = append(valid, email)
		} else {
			invalid = append(invalid, email)
		}
	}

	sum := participantSummary{
		Added:           []addedParticipant{},
		AlreadyEligible: []string{},
		Skipped:         []skippedParticipant{},
	}
	if len(valid) > 0 {
		var err error
		sum, err = a.addParticipants(r.Context(), electionID, valid)
		if err != nil {
			writeParticipantError(w, err)
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"added":            sum.Added,
		"already_eligible": sum.AlreadyEligible,
		"skipped":          sum.Skipped,
		"invalid":          invalid,
		"allowed_domain":   domain,
	})
}
