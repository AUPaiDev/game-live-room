package store

import (
	"fmt"

	"game-live-room/internal/model"

	"gorm.io/gorm"
)

// Store provides data access methods backed by GORM.
type Store struct {
	db *gorm.DB
}

// New creates a new Store with the given database connection.
func New(db *gorm.DB) *Store {
	return &Store{db: db}
}

// AutoMigrate runs GORM auto-migration for all models.
func (s *Store) AutoMigrate() error {
	return s.db.AutoMigrate(
		&model.LiveEvent{},
		&model.QuizQuestion{},
		&model.QuizSession{},
		&model.VoteRecord{},
		&model.VoteSession{},
	)
}

// SaveEvent persists a live event to the database.
func (s *Store) SaveEvent(event *model.LiveEvent) error {
	if err := s.db.Create(event).Error; err != nil {
		return fmt.Errorf("save event: %w", err)
	}
	return nil
}

// GetRecentEvents returns the most recent live events, optionally filtered by cmd.
func (s *Store) GetRecentEvents(limit int, cmd string) ([]model.LiveEvent, error) {
	q := s.db.Order("created_at DESC").Limit(limit)
	if cmd != "" {
		q = q.Where("cmd = ?", cmd)
	}
	var events []model.LiveEvent
	if err := q.Find(&events).Error; err != nil {
		return nil, fmt.Errorf("get recent events: %w", err)
	}
	return events, nil
}

// GetQuizQuestions returns quiz questions, optionally filtered by type.
func (s *Store) GetQuizQuestions(questionType string, limit int) ([]model.QuizQuestion, error) {
	q := s.db.Order("id DESC")
	if limit > 0 {
		q = q.Limit(limit)
	}
	if questionType != "" {
		q = q.Where("type = ?", questionType)
	}
	var questions []model.QuizQuestion
	if err := q.Find(&questions).Error; err != nil {
		return nil, fmt.Errorf("get quiz questions: %w", err)
	}
	return questions, nil
}

// GetQuizQuestionByID returns a single quiz question by ID.
func (s *Store) GetQuizQuestionByID(id uint64) (*model.QuizQuestion, error) {
	var q model.QuizQuestion
	if err := s.db.First(&q, id).Error; err != nil {
		return nil, fmt.Errorf("get quiz question %d: %w", id, err)
	}
	return &q, nil
}

// CreateQuizQuestion inserts a new quiz question.
func (s *Store) CreateQuizQuestion(q *model.QuizQuestion) error {
	if err := s.db.Create(q).Error; err != nil {
		return fmt.Errorf("create quiz question: %w", err)
	}
	return nil
}

// UpdateQuizQuestion updates an existing quiz question.
func (s *Store) UpdateQuizQuestion(q *model.QuizQuestion) error {
	if err := s.db.Save(q).Error; err != nil {
		return fmt.Errorf("update quiz question: %w", err)
	}
	return nil
}

// DeleteQuizQuestion removes a quiz question by ID.
func (s *Store) DeleteQuizQuestion(id uint64) error {
	if err := s.db.Delete(&model.QuizQuestion{}, id).Error; err != nil {
		return fmt.Errorf("delete quiz question %d: %w", id, err)
	}
	return nil
}

// SaveQuizSession inserts a new quiz session record.
func (s *Store) SaveQuizSession(sess *model.QuizSession) error {
	if err := s.db.Create(sess).Error; err != nil {
		return fmt.Errorf("save quiz session: %w", err)
	}
	return nil
}

// UpdateQuizSession updates an existing quiz session.
func (s *Store) UpdateQuizSession(sess *model.QuizSession) error {
	if err := s.db.Save(sess).Error; err != nil {
		return fmt.Errorf("update quiz session: %w", err)
	}
	return nil
}

// SaveVoteRecord inserts a new vote record.
func (s *Store) SaveVoteRecord(r *model.VoteRecord) error {
	if err := s.db.Create(r).Error; err != nil {
		return fmt.Errorf("save vote record: %w", err)
	}
	return nil
}

// GetVoteScores returns the total score per slot for a given session.
func (s *Store) GetVoteScores(sessionID string) (map[string]int, error) {
	type result struct {
		SlotID     string
		TotalScore int
	}
	var rows []result
	err := s.db.Model(&model.VoteRecord{}).
		Select("slot_id, SUM(score) as total_score").
		Where("session_id = ?", sessionID).
		Group("slot_id").
		Scan(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("get vote scores: %w", err)
	}
	scores := make(map[string]int, len(rows))
	for _, r := range rows {
		scores[r.SlotID] = r.TotalScore
	}
	return scores, nil
}

// CreateVoteSession inserts a new vote session.
func (s *Store) CreateVoteSession(sess *model.VoteSession) error {
	if err := s.db.Create(sess).Error; err != nil {
		return fmt.Errorf("create vote session: %w", err)
	}
	return nil
}

// UpdateVoteSession updates an existing vote session.
func (s *Store) UpdateVoteSession(sess *model.VoteSession) error {
	if err := s.db.Save(sess).Error; err != nil {
		return fmt.Errorf("update vote session: %w", err)
	}
	return nil
}
