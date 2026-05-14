package overlay

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"game-live-room/internal/hub"
	"game-live-room/internal/model"
	"game-live-room/internal/store"
)

// Manager holds the current overlay layout config in memory and persists it to DB.
type Manager struct {
	store *store.Store
	hub   *hub.Hub
	mu    sync.RWMutex
	cfg   LayoutConfig
}

// New creates a new overlay Manager.
func New(s *store.Store, h *hub.Hub) *Manager {
	return &Manager{
		store: s,
		hub:   h,
		cfg:   DefaultConfig(),
	}
}

// Load reads the config from DB into memory. Falls back to DefaultConfig if no row exists.
func (m *Manager) Load() error {
	row, err := m.store.GetOverlayConfig()
	if err != nil {
		return fmt.Errorf("overlay manager load: %w", err)
	}
	if row == nil {
		// No config saved yet — persist the default so the row exists.
		return m.persist(DefaultConfig())
	}
	var cfg LayoutConfig
	if err := json.Unmarshal([]byte(row.ConfigJSON), &cfg); err != nil {
		return fmt.Errorf("overlay manager parse config: %w", err)
	}
	m.mu.Lock()
	m.cfg = cfg
	m.mu.Unlock()
	return nil
}

// Get returns a copy of the current layout config.
func (m *Manager) Get() LayoutConfig {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.cfg
}

// Update persists a new config, updates the in-memory cache, and broadcasts to overlay clients.
func (m *Manager) Update(cfg LayoutConfig) error {
	if err := m.persist(cfg); err != nil {
		return err
	}
	m.mu.Lock()
	m.cfg = cfg
	m.mu.Unlock()
	m.broadcast(cfg)
	return nil
}

func (m *Manager) persist(cfg LayoutConfig) error {
	data, err := json.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal overlay config: %w", err)
	}
	row := &model.OverlayConfig{
		Key:        "current",
		ConfigJSON: string(data),
		UpdatedAt:  time.Now(),
	}
	return m.store.UpsertOverlayConfig(row)
}

func (m *Manager) broadcast(cfg LayoutConfig) {
	payload, _ := json.Marshal(cfg)
	m.hub.BroadcastTo(hub.ClientOverlay, hub.Message{
		Type:    "overlay_layout",
		Payload: json.RawMessage(payload),
	})
}
