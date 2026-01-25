package voice

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
)

const whisperEndpoint = "https://api.openai.com/v1/audio/transcriptions"

// WhisperSTT handles speech-to-text using OpenAI Whisper
type WhisperSTT struct {
	apiKey string
	model  string
	client *http.Client
}

// NewWhisperSTT creates a new Whisper STT instance
func NewWhisperSTT(apiKey string) *WhisperSTT {
	if apiKey == "" {
		apiKey = os.Getenv("OPENAI_API_KEY")
	}
	return &WhisperSTT{
		apiKey: apiKey,
		model:  "whisper-1",
		client: &http.Client{},
	}
}

// Transcribe converts audio to text
func (w *WhisperSTT) Transcribe(ctx context.Context, audio []byte) (string, error) {
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)

	// Add audio file
	part, err := writer.CreateFormFile("file", "audio.webm")
	if err != nil {
		return "", err
	}
	if _, err := part.Write(audio); err != nil {
		return "", err
	}

	// Add model field
	if err := writer.WriteField("model", w.model); err != nil {
		return "", err
	}

	// Add response format
	if err := writer.WriteField("response_format", "json"); err != nil {
		return "", err
	}

	if err := writer.Close(); err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", whisperEndpoint, &buf)
	if err != nil {
		return "", err
	}

	req.Header.Set("Authorization", "Bearer "+w.apiKey)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp, err := w.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("Whisper API error: %s", string(body))
	}

	var result struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", err
	}

	return result.Text, nil
}

// TranscribeStream handles streaming audio transcription
func (w *WhisperSTT) TranscribeStream(ctx context.Context, audioChan <-chan []byte) (<-chan string, <-chan error) {
	textChan := make(chan string)
	errChan := make(chan error, 1)

	go func() {
		defer close(textChan)
		defer close(errChan)

		var buffer []byte
		for audio := range audioChan {
			buffer = append(buffer, audio...)

			// Process when we have enough audio (e.g., 1 second)
			if len(buffer) >= 16000*2 { // 16kHz * 2 bytes per sample
				text, err := w.Transcribe(ctx, buffer)
				if err != nil {
					errChan <- err
					return
				}
				if text != "" {
					textChan <- text
				}
				buffer = nil
			}
		}

		// Process remaining audio
		if len(buffer) > 0 {
			text, err := w.Transcribe(ctx, buffer)
			if err != nil {
				errChan <- err
				return
			}
			if text != "" {
				textChan <- text
			}
		}
	}()

	return textChan, errChan
}
