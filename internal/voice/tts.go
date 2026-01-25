package voice

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
)

const ttsEndpoint = "https://api.openai.com/v1/audio/speech"

// OpenAITTS handles text-to-speech using OpenAI TTS
type OpenAITTS struct {
	apiKey string
	model  string
	voice  string
	client *http.Client
}

// NewOpenAITTS creates a new OpenAI TTS instance
func NewOpenAITTS(apiKey string) *OpenAITTS {
	if apiKey == "" {
		apiKey = os.Getenv("OPENAI_API_KEY")
	}
	return &OpenAITTS{
		apiKey: apiKey,
		model:  "tts-1",
		voice:  "alloy",
		client: &http.Client{},
	}
}

// SetVoice sets the TTS voice
func (t *OpenAITTS) SetVoice(voice string) {
	t.voice = voice
}

// SetModel sets the TTS model (tts-1 or tts-1-hd)
func (t *OpenAITTS) SetModel(model string) {
	t.model = model
}

// Synthesize converts text to audio
func (t *OpenAITTS) Synthesize(ctx context.Context, text string) ([]byte, error) {
	reqBody := map[string]string{
		"model": t.model,
		"input": text,
		"voice": t.voice,
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", ttsEndpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", "Bearer "+t.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := t.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		errBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("TTS API error: %s", string(errBody))
	}

	return io.ReadAll(resp.Body)
}

// SynthesizeStream streams audio synthesis
func (t *OpenAITTS) SynthesizeStream(ctx context.Context, text string) (<-chan []byte, <-chan error) {
	audioChan := make(chan []byte)
	errChan := make(chan error, 1)

	go func() {
		defer close(audioChan)
		defer close(errChan)

		audio, err := t.Synthesize(ctx, text)
		if err != nil {
			errChan <- err
			return
		}

		// Chunk the audio for streaming
		chunkSize := 4096
		for i := 0; i < len(audio); i += chunkSize {
			end := i + chunkSize
			if end > len(audio) {
				end = len(audio)
			}
			audioChan <- audio[i:end]
		}
	}()

	return audioChan, errChan
}
