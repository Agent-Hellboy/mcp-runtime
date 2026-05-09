package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"

	"mcp-runtime/pkg/events"
)

func (s *proxyServer) startAnalyticsDispatcher() {
	if s.analyticsURL == "" {
		return
	}
	s.analyticsOnce.Do(func() {
		s.analyticsMu.Lock()
		if s.analyticsQueue == nil {
			s.analyticsQueue = make(chan events.Envelope, analyticsQueueSize)
		}
		queue := s.analyticsQueue
		ctx, cancel := context.WithCancel(context.Background())
		s.analyticsCancel = cancel
		s.analyticsMu.Unlock()
		for i := 0; i < analyticsWorkerCount; i++ {
			go func() {
				for {
					select {
					case <-ctx.Done():
						return
					case event, ok := <-queue:
						if !ok {
							return
						}
						s.emit(ctx, event)
					}
				}
			}()
		}
	})
}

func (s *proxyServer) stopAnalyticsDispatcher() {
	s.analyticsMu.Lock()
	cancel := s.analyticsCancel
	s.analyticsMu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (s *proxyServer) emitIfEnabled(event events.Envelope) {
	if s.analyticsURL == "" {
		return
	}
	queue := s.analyticsEventQueue()
	if queue == nil {
		return
	}
	select {
	case queue <- event:
	default:
	}
}

func (s *proxyServer) analyticsEventQueue() chan events.Envelope {
	s.analyticsMu.Lock()
	queue := s.analyticsQueue
	s.analyticsMu.Unlock()
	if queue != nil {
		return queue
	}

	if s.analyticsURL != "" {
		s.startAnalyticsDispatcher()
	}
	s.analyticsMu.Lock()
	defer s.analyticsMu.Unlock()
	return s.analyticsQueue
}

// emit sends analytics events to the ingest service.
func (s *proxyServer) emit(ctx context.Context, event events.Envelope) {
	data, err := json.Marshal(event)
	if err != nil {
		return
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.analyticsURL, bytes.NewReader(data))
	if err != nil {
		return
	}
	req.Header.Set("content-type", "application/json")
	if s.apiKey != "" {
		req.Header.Set("x-api-key", s.apiKey)
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		log.Printf("failed to emit proxy analytics event to %s: %v", s.analyticsURL, err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		log.Printf("proxy analytics emission failed with status %d to %s", resp.StatusCode, s.analyticsURL)
		_, _ = io.Copy(io.Discard, resp.Body)
		return
	}
	_, _ = io.Copy(io.Discard, resp.Body)
}
