package scheduler

import (
	"context"
	"github.com/fethiaksit/social-analytics/internal/instagram"
	"github.com/fethiaksit/social-analytics/internal/services"
	"github.com/robfig/cron/v3"
	"log"
	"time"
)

type Scheduler struct {
	cron                *cron.Cron
	service             *services.Service
	instagram           *instagram.Service
	instagramExpression string
	expression          string
}

func New(s *services.Service, expression string, instagramService *instagram.Service, instagramExpression string) *Scheduler {
	return &Scheduler{cron: cron.New(), service: s, expression: expression, instagram: instagramService, instagramExpression: instagramExpression}
}

// Start schedules account scans; processor extensions can add analysis backfill and retry jobs here.
func (s *Scheduler) Start() {
	_, err := s.cron.AddFunc(s.expression, func() {
		if e := s.service.ScanActiveAccounts(context.Background()); e != nil {
			log.Printf("scan failed: %v", e)
		}
	})
	if err != nil {
		log.Printf("invalid cron: %v", err)
		return
	}
	s.cron.Start()
	if s.instagram == nil || !s.instagram.Configured() {
		return
	}
	_, err = s.cron.AddFunc(s.instagramExpression, func() {
		ctx, cancel := context.WithTimeout(context.Background(), 9*time.Minute)
		defer cancel()
		if e := s.instagram.SyncActive(ctx); e != nil {
			log.Printf("Instagram scheduled sync failed: %v", e)
		}
	})
	if err != nil {
		log.Printf("invalid Instagram cron: %v", err)
	}
}
func (s *Scheduler) Stop() { s.cron.Stop() }
