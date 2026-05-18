package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/viper"
)

// Config is the root configuration structure.
type Config struct {
	Server   ServerConfig   `mapstructure:"server"`
	MySQL    MySQLConfig    `mapstructure:"mysql"`
	Bilibili BilibiliConfig `mapstructure:"bilibili"`
	Game     GameConfig     `mapstructure:"game"`
}

// ServerConfig holds HTTP server settings.
type ServerConfig struct {
	Port       int    `mapstructure:"port"`
	AdminToken string `mapstructure:"admin_token"`
}

// MySQLConfig holds database connection settings.
type MySQLConfig struct {
	Host     string `mapstructure:"host"`
	Port     int    `mapstructure:"port"`
	User     string `mapstructure:"user"`
	Password string `mapstructure:"password"`
	Database string `mapstructure:"database"`
}

// DSN returns the MySQL data source name.
// If the MYSQL_DSN environment variable is set, it takes precedence.
func (m *MySQLConfig) DSN() string {
	if dsn := os.Getenv("MYSQL_DSN"); dsn != "" {
		return dsn
	}
	return fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?charset=utf8mb4&parseTime=True&loc=Local",
		m.User, m.Password, m.Host, m.Port, m.Database)
}

// BilibiliConfig holds B站 connection settings.
type BilibiliConfig struct {
	RoomID       uint64 `mapstructure:"room_id"`
	Cookie       string `mapstructure:"cookie"`
	UserAgent    string `mapstructure:"user_agent"`
	DanmakuWSURL string `mapstructure:"danmaku_ws_url"`
}

// GameConfig holds all game module configurations.
type GameConfig struct {
	GiftTrigger GiftTriggerConfig `mapstructure:"gift_trigger"`
	SCTrigger   SCTriggerConfig   `mapstructure:"sc_trigger"`
	Quiz        QuizConfig        `mapstructure:"quiz"`
	Vote        VoteConfig        `mapstructure:"vote"`
	Mic         MicConfig         `mapstructure:"mic"`
}

// GiftTriggerConfig configures the gift trigger module.
type GiftTriggerConfig struct {
	Enabled bool       `mapstructure:"enabled"`
	Rules   []GiftRule `mapstructure:"rules"`
}

// GiftRule maps a gift name to a triggered action.
type GiftRule struct {
	GiftName string                 `mapstructure:"gift_name"`
	Action   string                 `mapstructure:"action"`
	Params   map[string]interface{} `mapstructure:"params"`
}

// SCTriggerConfig configures the SuperChat trigger module.
type SCTriggerConfig struct {
	Enabled  bool `mapstructure:"enabled"`
	MinPrice int  `mapstructure:"min_price"`
}

// QuizConfig configures the quiz game module.
type QuizConfig struct {
	Enabled          bool    `mapstructure:"enabled"`
	AnswerTimeout    int     `mapstructure:"answer_timeout"`
	MinCorrectRatio  float64 `mapstructure:"min_correct_ratio"`
}

// VoteConfig configures the gift vote module.
type VoteConfig struct {
	Enabled bool       `mapstructure:"enabled"`
	Slots   []VoteSlot `mapstructure:"slots"`
}

// VoteSlot represents a voting position (麦位).
type VoteSlot struct {
	ID       string `mapstructure:"id"`
	Name     string `mapstructure:"name"`
	GiftName string `mapstructure:"gift_name"`
}

// MicConfig configures the mic queue karaoke module.
type MicConfig struct {
	Enabled  bool   `mapstructure:"enabled"`
	Keyword  string `mapstructure:"keyword"`
	GiftName string `mapstructure:"gift_name"`
}

// Load reads configuration from config.yaml and applies environment variable overrides.
func Load() (*Config, error) {
	v := viper.New()
	v.SetConfigName("config")
	v.SetConfigType("yaml")
	v.AddConfigPath(".")
	v.AddConfigPath("/app")

	setDefaults(v)

	if err := v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return nil, fmt.Errorf("read config: %w", err)
		}
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}

	applyEnvOverrides(&cfg)
	return &cfg, nil
}

func setDefaults(v *viper.Viper) {
	v.SetDefault("server.port", 8080)
	v.SetDefault("mysql.host", "127.0.0.1")
	v.SetDefault("mysql.port", 3306)
	v.SetDefault("mysql.user", "root")
	v.SetDefault("mysql.database", "game_live_room")
	v.SetDefault("bilibili.user_agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")
	v.SetDefault("bilibili.danmaku_ws_url", "wss://broadcastlv.chat.bilibili.com/sub")
	v.SetDefault("game.sc_trigger.min_price", 30)
	v.SetDefault("game.quiz.answer_timeout", 30)
	v.SetDefault("game.quiz.min_correct_ratio", 0.1)
	v.SetDefault("game.mic.keyword", "报名")
	v.SetDefault("game.mic.gift_name", "小花花")
}

func applyEnvOverrides(cfg *Config) {
	if roomID := os.Getenv("BILI_ROOM_ID"); roomID != "" {
		if id, err := strconv.ParseUint(roomID, 10, 64); err == nil {
			cfg.Bilibili.RoomID = id
		}
	}
	if cookie := os.Getenv("BILI_COOKIE"); cookie != "" {
		cfg.Bilibili.Cookie = cookie
	}
	if port := os.Getenv("SERVER_PORT"); port != "" {
		if p, err := strconv.Atoi(port); err == nil {
			cfg.Server.Port = p
		}
	}
	if token := os.Getenv("ADMIN_TOKEN"); token != "" {
		cfg.Server.AdminToken = token
	}
}

// ExtractUID parses the DedeUserID field from a B站 cookie string.
func ExtractUID(cookie string) uint64 {
	for _, part := range strings.Split(cookie, ";") {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(part, "DedeUserID=") {
			val := strings.TrimPrefix(part, "DedeUserID=")
			if uid, err := strconv.ParseUint(val, 10, 64); err == nil {
				return uid
			}
		}
	}
	return 0
}
