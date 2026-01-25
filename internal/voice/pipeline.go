package voice

import (
	"context"
	"log"
	"sync"
)

// Pipeline orchestrates voice processing (STT -> LLM -> TTS)
type Pipeline struct {
	stt *WhisperSTT
	tts *OpenAITTS

	mu       sync.RWMutex
	sessions map[string]*Session
}

// Session represents an active voice session
type Session struct {
	ID          string
	UserID      string
	AudioBuffer []byte
	Processing  bool
}

// NewPipeline creates a new voice pipeline
func NewPipeline(openaiKey string) *Pipeline {
	return &Pipeline{
		stt:      NewWhisperSTT(openaiKey),
		tts:      NewOpenAITTS(openaiKey),
		sessions: make(map[string]*Session),
	}
}

// StartSession creates a new voice session
func (p *Pipeline) StartSession(sessionID, userID string) *Session {
	p.mu.Lock()
	defer p.mu.Unlock()

	session := &Session{
		ID:     sessionID,
		UserID: userID,
	}
	p.sessions[sessionID] = session
	log.Printf("[Voice] Session started: %s", sessionID)
	return session
}

// EndSession ends a voice session
func (p *Pipeline) EndSession(sessionID string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.sessions, sessionID)
	log.Printf("[Voice] Session ended: %s", sessionID)
}

// GetSession returns an existing session
func (p *Pipeline) GetSession(sessionID string) (*Session, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	session, ok := p.sessions[sessionID]
	return session, ok
}

// ProcessAudio converts audio to text using Whisper
func (p *Pipeline) ProcessAudio(ctx context.Context, sessionID string, audio []byte) (string, error) {
	session, ok := p.GetSession(sessionID)
	if !ok {
		return "", nil
	}

	session.Processing = true
	defer func() { session.Processing = false }()

	// Transcribe audio
	text, err := p.stt.Transcribe(ctx, audio)
	if err != nil {
		return "", err
	}

	log.Printf("[Voice] Transcribed: %s", text)
	return text, nil
}

// GenerateSpeech converts text to audio using OpenAI TTS
func (p *Pipeline) GenerateSpeech(ctx context.Context, text string) ([]byte, error) {
	return p.tts.Synthesize(ctx, text)
}

// StreamingResult represents a streaming TTS result
type StreamingResult struct {
	Audio []byte
	Done  bool
	Error error
}
