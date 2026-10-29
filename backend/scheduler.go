package main

import (
	"context"
	"log"
	"time"
)

// How often the clock is checked. A few seconds is plenty: ballots are also
// checked against the voting window themselves (see handleSubmitBallot), so a
// late tick can never let a vote in outside the window.
const electionSchedulerInterval = 5 * time.Second

// advanceElectionStatuses moves elections forward according to the clock:
//
//   - registration -> active_voting once voting_start has passed,
//   - registration/active_voting -> completed once voting_end has passed.
//
// A draft election is never touched: the admin publishes it by opening
// registration. Statuses only ever move forward here, so an admin closing an
// election early with the manual control is respected.
func (a *App) advanceElectionStatuses(ctx context.Context) (closed, opened int64, err error) {
	// Close first, so an election whose whole window has already passed goes
	// straight to completed instead of briefly opening.
	tag, err := a.db.Exec(ctx, `
		UPDATE evoting.elections
		SET status = 'completed'
		WHERE status IN ('registration', 'active_voting') AND voting_end <= NOW()`)
	if err != nil {
		return 0, 0, err
	}
	closed = tag.RowsAffected()

	tag, err = a.db.Exec(ctx, `
		UPDATE evoting.elections
		SET status = 'active_voting'
		WHERE status = 'registration' AND voting_start <= NOW() AND voting_end > NOW()`)
	if err != nil {
		return closed, 0, err
	}
	return closed, tag.RowsAffected(), nil
}

// runElectionScheduler checks the clock immediately and then every interval
// until ctx is cancelled. It is safe to run on several backend instances: the
// updates are idempotent.
func (a *App) runElectionScheduler(ctx context.Context) {
	ticker := time.NewTicker(electionSchedulerInterval)
	defer ticker.Stop()
	for {
		closed, opened, err := a.advanceElectionStatuses(ctx)
		switch {
		case ctx.Err() != nil:
			return
		case err != nil:
			log.Printf("election scheduler: %v", err)
		case closed > 0 || opened > 0:
			log.Printf("election scheduler: opened %d, closed %d", opened, closed)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
