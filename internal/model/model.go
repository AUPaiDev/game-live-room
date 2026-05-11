package model

import "time"

// LiveEvent records a danmaku or gift event from the live room.
type LiveEvent struct {
	ID         uint64    `gorm:"primaryKey;autoIncrement"`
	Cmd        string    `gorm:"size:64;not null;index"`
	UID        uint64    `gorm:"index"`
	Username   string    `gorm:"size:128"`
	Text       string    `gorm:"type:text"`
	GiftName   string    `gorm:"size:128"`
	GiftCount  int
	Price      int       // 金瓜子(礼物) or RMB元(SC/舰长)
	GuardLevel int
	CreatedAt  time.Time `gorm:"index"`
}

// QuizQuestion is a question in the quiz question bank.
type QuizQuestion struct {
	ID          uint64    `gorm:"primaryKey;autoIncrement"`
	Type        string    `gorm:"size:16;not null"` // text/image/audio
	Question    string    `gorm:"type:text;not null"`
	Options     string    `gorm:"type:json"`   // JSON []string
	Answer      int       `gorm:"not null"`    // correct option index (0-based)
	MediaPath   string    `gorm:"size:512"`    // image/audio path
	Explanation string    `gorm:"type:text"`
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// QuizSession records a single quiz game round.
type QuizSession struct {
	ID           uint64     `gorm:"primaryKey;autoIncrement"`
	QuestionID   uint64     `gorm:"index"`
	StartedAt    time.Time
	EndedAt      *time.Time
	TotalAnswers int
	CorrectCount int
}

// VoteRecord records a single gift vote action.
type VoteRecord struct {
	ID        uint64    `gorm:"primaryKey;autoIncrement"`
	SessionID string    `gorm:"size:64;index"`
	SlotID    string    `gorm:"size:64;index"`
	UID       uint64
	Username  string    `gorm:"size:128"`
	GiftName  string    `gorm:"size:128"`
	GiftCount int
	Score     int
	CreatedAt time.Time
}

// VoteSession represents a gift vote game session.
type VoteSession struct {
	ID        string     `gorm:"primaryKey;size:64"`
	Status    string     `gorm:"size:16"` // active/ended
	Config    string     `gorm:"type:json"` // JSON VoteConfig snapshot
	StartedAt time.Time
	EndedAt   *time.Time
}
