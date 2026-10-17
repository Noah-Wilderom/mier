package lspclient

import (
	"context"
	"errors"
	"strings"
)

// RestartAnalysis drops live sessions/grants. A subsequent capture validates
// dependencies again before deciding whether a persisted snapshot can be reused.
func (m *Manager) RestartAnalysis(ctx context.Context, root string) error {
	root, err := canonicalRoot(root)
	if err != nil {
		return err
	}
	release, err := m.lock(ctx)
	if err != nil {
		return err
	}
	defer release()
	for _, w := range m.workspaces {
		if w.root == root && w.leases > 0 {
			return errors.New("A symbol lookup is still active. Wait for it to finish, then restart analysis")
		}
	}
	for key, w := range m.workspaces {
		if w.root == root {
			w.close()
			delete(m.workspaces, key)
		}
	}
	order := m.order[:0]
	for _, key := range m.order {
		if m.workspaces[key] != nil {
			order = append(order, key)
		}
	}
	m.order = order
	for alias, target := range m.aliases {
		if m.workspaces[target] == nil {
			delete(m.aliases, alias)
		}
	}
	for uri, grant := range m.grants {
		if grant.workspace.root == root {
			m.grantBytes -= len(grant.text)
			delete(m.grants, uri)
		}
	}
	grants := m.grantOrder[:0]
	for _, uri := range m.grantOrder {
		if _, ok := m.grants[uri]; ok {
			grants = append(grants, uri)
		}
	}
	m.grantOrder = grants
	m.warmupMu.Lock()
	for key := range m.analysis {
		if strings.HasPrefix(key, root+"\x00") {
			delete(m.analysis, key)
		}
	}
	m.warmupMu.Unlock()
	return nil
}
