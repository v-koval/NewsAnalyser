package scheduler

import (
	"context"
	"log"
	"time"

	"newsanalyzer/internal/processor"
	"newsanalyzer/internal/repo"
)

type Scheduler struct {
	Repo      *repo.Repo
	Processor *processor.Processor
	trigger   chan string
}

func New(r *repo.Repo, p *processor.Processor) *Scheduler {
	return &Scheduler{Repo: r, Processor: p, trigger: make(chan string, 32)}
}

func (s *Scheduler) Trigger(digestID string) {
	select {
	case s.trigger <- digestID:
	default:
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
			s.runOne(ctx, id)
		}
	}
}

func (s *Scheduler) tick(ctx context.Context) {
	settings, err := s.Repo.GetSettings(ctx)
	if err != nil {
		log.Printf("scheduler settings: %v", err)
		return
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
		if d.LastRunAt != nil && now.Sub(d.LastRunAt.UTC()) < time.Duration(d.FrequencyHours)*time.Hour {
			continue
		}
		go s.runOne(ctx, d.ID)
	}
}

func (s *Scheduler) runOne(ctx context.Context, id string) {
	log.Printf("runOne: starting digest %s", id)
	settings, err := s.Repo.GetSettings(ctx)
	if err != nil {
		log.Printf("runOne: get settings: %v", err)
		return
	}
	if settings.ProcessingPaused {
		log.Printf("runOne: processing paused, skipping")
		return
	}
	runCtx, cancel := context.WithTimeout(ctx, 35*time.Minute)
	defer cancel()
	if err := s.Processor.Run(runCtx, id); err != nil {
		log.Printf("run digest %s: %v", id, err)
	}
}
