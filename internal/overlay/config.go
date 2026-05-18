package overlay

// ModulePosition holds the position of a module on the 1920×1080 canvas.
type ModulePosition struct {
	Preset string  `json:"preset"` // top-left|top-right|center|bottom-left|bottom-right|right|custom
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
}

// ModuleConfig holds the display configuration for a single overlay module.
type ModuleConfig struct {
	Visible  bool           `json:"visible"`
	Position ModulePosition `json:"position"`
	Theme    string         `json:"theme"` // cyberpunk|warm|glass
}

// LayoutConfig is the full overlay layout and theme configuration.
type LayoutConfig struct {
	Modules map[string]ModuleConfig `json:"modules"`
}

// DefaultConfig returns a LayoutConfig that mirrors the hardcoded CSS positions
// in overlay/index.html, so the overlay looks identical before any customization.
func DefaultConfig() LayoutConfig {
	return LayoutConfig{
		Modules: map[string]ModuleConfig{
			"gift-alert": {
				Visible:  false,
				Position: ModulePosition{Preset: "top-right", X: 1504, Y: 36},
				Theme:    "cyberpunk",
			},
			"sc-alert": {
				Visible:  false,
				Position: ModulePosition{Preset: "top-right", X: 1484, Y: 130},
				Theme:    "cyberpunk",
			},
			"scoreboard": {
				Visible:  false,
				Position: ModulePosition{Preset: "right", X: 1584, Y: 240},
				Theme:    "cyberpunk",
			},
			"quiz-display": {
				Visible:  false,
				Position: ModulePosition{Preset: "center", X: 600, Y: 360},
				Theme:    "cyberpunk",
			},
			"quiz-result": {
				Visible:  false,
				Position: ModulePosition{Preset: "center", X: 650, Y: 360},
				Theme:    "cyberpunk",
			},
			"danmaku-wall": {
				Visible:  true,
				Position: ModulePosition{Preset: "bottom-left", X: 36, Y: 664},
				Theme:    "cyberpunk",
			},
			"mic-singer": {
				Visible:  false,
				Position: ModulePosition{Preset: "top-left", X: 36, Y: 36},
				Theme:    "cyberpunk",
			},
			"mic-leaderboard": {
				Visible:  false,
				Position: ModulePosition{Preset: "top-left", X: 36, Y: 140},
				Theme:    "cyberpunk",
			},
			"mic-inactive": {
				Visible:  false,
				Position: ModulePosition{Preset: "bottom-left", X: 36, Y: 1008},
				Theme:    "cyberpunk",
			},
		},
	}
}
