package lspclient

import (
	"context"
	"errors"
	"strconv"
	"strings"
)

type warmupJob struct {
	done chan struct{}
	err  error
}

// Warmup prepares a visible document before its first symbol request. Work is
// owned by the application: cancelling a waiting webview request cannot discard
// a captured revision or interrupt language-server initialization.
func (m *Manager) Warmup(ctx context.Context, request CodePositionRequest) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	root, err := canonicalRoot(request.Root)
	if err != nil {
		return err
	}
	request.Root = root
	key := strings.Join([]string{root, request.Revision, request.ComparisonHead, request.Host, request.Owner, request.Repo, strconv.Itoa(request.PullRequest), request.Path}, "\x00")
	m.warmupMu.Lock()
	job := m.warmups[key]
	if job == nil {
		if m.ctx.Err() != nil {
			m.warmupMu.Unlock()
			return errors.New("Code intelligence has shut down")
		}
		if len(m.warmups) >= 4 {
			m.warmupMu.Unlock()
			return errors.New("Code analysis is preparing other files. Try again shortly")
		}
		job = &warmupJob{done: make(chan struct{})}
		m.warmups[key] = job
		go func() {
			workCtx, cancel := m.requestContext(m.ctx)
			defer cancel()
			_, s, path, text, language, done, err := m.prepare(workCtx, request)
			if err == nil {
				unlock, lockErr := s.lock(workCtx)
				if lockErr != nil {
					err = lockErr
				} else {
					if s.peer != nil {
						err = s.peer.open(workCtx, path, language, text)
					}
					if err == nil {
						err = s.open(workCtx, path, language, text)
					}
					unlock()
				}
				done()
			}
			m.warmupMu.Lock()
			job.err = err
			delete(m.warmups, key)
			close(job.done)
			m.warmupMu.Unlock()
		}()
	}
	m.warmupMu.Unlock()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-job.done:
		return job.err
	}
}
