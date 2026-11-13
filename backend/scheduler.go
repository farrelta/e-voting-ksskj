package main

import (
	"context"
	"log"
	"time"
)

// How often the clock is checked
const electionSchedulerInterval = 5 * time.Second

// advanceElectionStatuses moves elections forward according to the clock. A draft election is never touched: the admin publishes it by opening registration.
func (a *App) advanceElectionStatuses(ctx context.Context) (closed, opened int64, err error) {
	tag, err := a.db.Exec(ctx, `
		UPDATE evoting.elections
		SET status = 'completed'
		WHERE status IN ('registration', 'active_voting') AND voting_end <= NOW()`)
	if err != nil {
		return 0, 0, err
	}
	closed = tag.RowsAffected()

	tag, err = a.db.Exec(ctx, `
		UPDATE evoting.elections e
		SET status = 'active_voting'
		WHERE status = 'registration' AND voting_start <= NOW() AND voting_end > NOW()
			AND EXISTS (SELECT 1 FROM evoting.anonymous_issuer_keys k WHERE k.election_id = e.id)`)
	if err != nil {
		return closed, 0, err
	}
	return closed, tag.RowsAffected(), nil
}

// runElectionScheduler checks the clock immediately and then every interval until ctx is cancelled.
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
