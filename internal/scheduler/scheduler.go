package scheduler

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"time"

	"newsanalyzer/internal/processor"
	"newsanalyzer/internal/repo"
)

type Scheduler struct {
	Repo        *repo.Repo
	Processor   *processor.Processor
	ImagesDir   string
	trigger     chan string
	lastCleanup time.Time
}

func New(r *repo.Repo, p *processor.Processor, imagesDir string) *Scheduler {
	return &Scheduler{Repo: r, Processor: p, ImagesDir: imagesDir, trigger: make(chan string, 32)}
}

// Trigger queues a manual run. It reports false when the queue is full so the
// caller can tell the user instead of silently dropping the run.
func (s *Scheduler) Trigger(digestID string) bool {
	select {
	case s.trigger <- digestID:
		return true
	default:
		return false
	}
}

func (s *Scheduler) Start(ctx context.Context) {
	go s.loop(ctx)
}

func (s *Scheduler) loop(ctx context.Context) {
	s.tick(ctx)
	t := time.NewTicker(time.Hour)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.tick(ctx)
		case id := <-s.trigger:
			go s.runOne(ctx, id, false)
		}
	}
}

func (s *Scheduler) tick(ctx context.Context) {
	settings, err := s.Repo.GetSettings(ctx)
	if err != nil {
		log.Printf("scheduler settings: %v", err)
		return
	}
	if time.Since(s.lastCleanup) >= 24*time.Hour {
		s.cleanup(ctx, settings.KeepRunsDays)
		s.lastCleanup = time.Now()
	}
	if settings.ProcessingPaused {
		return
	}
	digests, err := s.Repo.ListDigests(ctx)
	if err != nil {
		log.Printf("scheduler list: %v", err)
		return
	}
	now := time.Now().UTC()
	for _, d := range digests {
		if !d.Enabled {
			continue
		}
		freq := time.Duration(d.FrequencyHours) * time.Hour

		// First-ever run: next_run_at is NULL, fire immediately.
		if d.NextRunAt == nil {
			go s.runOne(ctx, d.ID, true)
			continue
		}

		next := d.NextRunAt.UTC()

		// Slot still in the future — wait.
		if now.Before(next) {
			continue
		}

		// Slot is more than one full frequency in the past: missed slots are
		// dropped. Jump next_run_at forward to the nearest future grid point
		// without running.
		if now.Sub(next) >= freq {
			for !next.After(now) {
				next = next.Add(freq)
			}
			if err := s.Repo.SetDigestNextRun(ctx, d.ID, next); err != nil {
				log.Printf("scheduler: jump next_run_at for digest %s: %v", d.ID, err)
			}
			continue
		}

		// Normal slot: next_run_at <= now < next_run_at + freq.
		go s.runOne(ctx, d.ID, true)
	}
}

// cleanup prunes expired refresh tokens and, when retention is configured,
// removes image directories of runs older than keepDays and then deletes the
// rows; a failed directory removal keeps the row so the next pass retries it.
func (s *Scheduler) cleanup(ctx context.Context, keepDays int) {
	if n, err := s.Repo.DeleteExpiredRefresh(ctx); err != nil {
		log.Printf("cleanup refresh tokens: %v", err)
	} else if n > 0 {
		log.Printf("cleanup: deleted %d expired refresh tokens", n)
	}
	if keepDays <= 0 {
		return
	}
	before := time.Now().UTC().AddDate(0, 0, -keepDays)
	ids, err := s.Repo.ListOldRunIDs(ctx, before)
	if err != nil {
		log.Printf("cleanup list old runs: %v", err)
		return
	}
	if len(ids) == 0 {
		return
	}
	deletable := make([]string, 0, len(ids))
	for _, id := range ids {
		if err := os.RemoveAll(filepath.Join(s.ImagesDir, id)); err != nil {
			log.Printf("cleanup images %s: %v (run kept for retry)", id, err)
			continue
		}
		deletable = append(deletable, id)
	}
	if len(deletable) == 0 {
		return
	}
	if err := s.Repo.DeleteRunsByID(ctx, deletable); err != nil {
		log.Printf("cleanup delete runs: %v", err)
		return
	}
	log.Printf("cleanup: deleted %d runs older than %d days", len(deletable), keepDays)
}

func (s *Scheduler) runOne(ctx context.Context, id string, scheduled bool) {
	log.Printf("runOne: starting digest %s (scheduled=%v)", id, scheduled)
	settings, err := s.Repo.GetSettings(ctx)
	if err != nil {
		log.Printf("runOne: get settings: %v", err)
		return
	}
	if settings.ProcessingPaused {
		log.Printf("runOne: processing paused, skipping")
		return
	}
	runCtx, cancel := context.WithTimeout(ctx, 50*time.Minute)
	defer cancel()
	if err := s.Processor.Run(runCtx, id, scheduled); err != nil {
		log.Printf("run digest %s: %v", id, err)
	}
}
