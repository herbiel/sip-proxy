package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/zaf/resample"
	"google.golang.org/genai"
)

const (
	// GEMINI_DEBUG enables detailed logging of all Gemini messages (sent and received)
	GEMINI_DEBUG = false
)

// GeminiHandler manages Gemini AI connections for audio processing
type GeminiHandler struct {
	mediaBridge       MediaBridge
	participant       *BaseParticipant
	participantID     string
	stopChan          chan struct{}
	ctx               context.Context
	cancel            context.CancelFunc
	client            *genai.Client
	session           *genai.Session
	wg                sync.WaitGroup
	audioWriter       *GeminiAudioWriter
	sessionClosed     bool
	sessionMu         sync.RWMutex
	sessionConfig     *SessionConfig
	firstDataReceived bool
	firstDataMu       sync.Mutex
}

// GeminiAudioWriter implements io.Writer to forward PCM audio to Gemini
type GeminiAudioWriter struct {
	handler *GeminiHandler
}

// Write receives PCM audio data and forwards it to Gemini
func (w *GeminiAudioWriter) Write(pcmData []byte) (n int, err error) {
	// Commented out for performance - uncomment if debugging audio write
	// slog.Debug("Gemini audio write",
	// 	"event", "gemini_audio_write",
	// 	"participant", w.handler.participantID,
	// 	"size_bytes", len(pcmData))

	w.handler.sessionMu.RLock()
	sessionClosed := w.handler.sessionClosed
	session := w.handler.session
	w.handler.sessionMu.RUnlock()

	if session == nil || sessionClosed {
		// Return io.ErrClosedPipe to signal a terminal error that should stop retries
		return 0, io.ErrClosedPipe
	}

	if len(pcmData) == 0 {
		slog.Warn("Empty PCM data received",
			"event", "gemini_audio_warn",
			"participant", w.handler.participantID)
		return len(pcmData), nil // Don't treat as error
	}

	// Check if this is the first data received and send Hello message
	w.handler.firstDataMu.Lock()
	if !w.handler.firstDataReceived {
		w.handler.firstDataReceived = true
		w.handler.firstDataMu.Unlock()

		// Send initial "Hello" message to start the conversation
		turnComplete := true
		err := session.SendClientContent(genai.LiveClientContentInput{
			Turns: []*genai.Content{
				genai.NewContentFromText("Hello", genai.RoleUser),
			},
			TurnComplete: &turnComplete,
		})
		if err != nil {
			slog.Warn("Failed to send initial Hello message",
				"event", "hello_message_failed",
				"participant", w.handler.participantID,
				"error", err.Error())
			// Don't return error, continue anyway
		} else {
			slog.Info("Sent initial Hello message to Gemini",
				"event", "hello_message_sent",
				"participant", w.handler.participantID)
		}
	} else {
		w.handler.firstDataMu.Unlock()
	}

	// Resample from 8000 Hz to 16000 Hz for Gemini
	// Gemini expects 16kHz PCM audio
	// Create a local resampler for this packet
	var outputBuf bytes.Buffer
	resampler, err := resample.New(&outputBuf, 8000.0, 16000.0, 1, resample.I16, resample.HighQ)
	if err != nil {
		slog.Error("Failed to create 8kHz->16kHz resampler",
			"event", "resample_error",
			"participant", w.handler.participantID,
			"error", err.Error())
		return 0, err
	}

	_, err = resampler.Write(pcmData)
	if err != nil {
		slog.Error("Failed to write audio for resampling to 16kHz",
			"event", "resample_error",
			"participant", w.handler.participantID,
			"error", err.Error())
		return 0, err
	}

	// Close to flush the resampler buffer to outputBuf
	err = resampler.Close()
	if err != nil {
		slog.Error("Failed to resample audio to 16kHz",
			"event", "resample_error",
			"participant", w.handler.participantID,
			"error", err.Error())
		return 0, err
	}

	resampledAudio := outputBuf.Bytes()

	// Send to Gemini using LiveRealtimeInput
	// Linear PCM 16-bit at 16000 Hz sample rate
	realtimeInput := genai.LiveRealtimeInput{
		Audio: &genai.Blob{
			MIMEType: "audio/pcm;rate=16000",
			Data:     resampledAudio,
		},
	}

	if err := w.handler.session.SendRealtimeInput(realtimeInput); err != nil {
		slog.Error("Error sending audio to Gemini",
			"event", "gemini_audio_error",
			"participant", w.handler.participantID,
			"error", err.Error())
		return 0, err
	}

	// Commented out for performance - uncomment if debugging audio sent
	
	if GEMINI_DEBUG {	
		slog.Debug("Successfully sent PCM to Gemini",
		"event", "gemini_audio_sent",
		"participant", w.handler.participantID,
		"resampled_bytes", len(resampledAudio),
		"original_bytes", len(pcmData))
	}

	return len(pcmData), nil
}

// NewGeminiHandler creates a new Gemini handler
func NewGeminiHandler(mediaBridge MediaBridge, participantID string, sessionConfig *SessionConfig) (*GeminiHandler, error) {
	slog.Info("Creating Gemini handler",
		"event", "gemini_handler_create",
		"participant", participantID)

	ctx, cancel := context.WithCancel(context.Background())

	// Get API key from environment
	apiKey := os.Getenv("GOOGLE_API_KEY")
	if apiKey == "" {
		cancel()
		return nil, fmt.Errorf("GOOGLE_API_KEY environment variable is not set")
	}

	slog.Info("Using Google API key from environment",
		"event", "api_key_loaded",
		"key_length", len(apiKey))

	// Initialize Gemini client with API key
	// The genai package expects the API key to be set in GOOGLE_API_KEY env var
	// or passed via ClientConfig
	client, err := genai.NewClient(ctx, &genai.ClientConfig{
		APIKey: apiKey,
	})
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to create Gemini client: %w", err)
	}

	handler := &GeminiHandler{
		mediaBridge:   mediaBridge,
		participantID: participantID,
		stopChan:      make(chan struct{}),
		ctx:           ctx,
		cancel:        cancel,
		client:        client,
		sessionConfig: sessionConfig,
	}

	// Create audio writer
	handler.audioWriter = &GeminiAudioWriter{handler: handler}

	// Create participant for Gemini with the audio writer
	handler.participant = NewParticipant(participantID, handler.audioWriter)

	// Add participant to media bridge
	if err := mediaBridge.AddParticipant(handler.participant); err != nil {
		cancel()
		return nil, fmt.Errorf("failed to add Gemini participant: %w", err)
	}

	slog.Info("Gemini handler created successfully",
		"event", "gemini_handler_created",
		"participant", participantID)

	return handler, nil
}

// Start begins processing audio with Gemini
func (g *GeminiHandler) Start() error {
	slog.Info("Starting Gemini handler",
		"event", "gemini_handler_start",
		"participant", g.participantID)

	// Determine model based on backend
	model := "gemini-live-2.5-flash-preview"

	slog.Info("Connecting to Gemini model",
		"event", "gemini_connect",
		"model", model)

	// Use system instructions from session config, or default if not provided
	systemInstructions := "You are a helpful voice assistant. Respond naturally to the user's audio input."
	if g.sessionConfig != nil && g.sessionConfig.SystemInstructions != "" {
		systemInstructions = g.sessionConfig.SystemInstructions
		slog.Info("Using system instructions from session config",
			"event", "system_instructions_config",
			"instructions", systemInstructions)
	} else {
		slog.Info("Using default system instructions",
			"event", "system_instructions_default",
			"instructions", systemInstructions)
	}

	// Build SpeechConfig if voice or language is provided
	var speechConfig *genai.SpeechConfig
	if g.sessionConfig != nil && (g.sessionConfig.Voice != "" || g.sessionConfig.Language != "") {
		speechConfig = &genai.SpeechConfig{}

		if g.sessionConfig.Language != "" {
			speechConfig.LanguageCode = g.sessionConfig.Language
			slog.Info("Using language from session config",
				"event", "speech_config_language",
				"language", g.sessionConfig.Language)
		}

		if g.sessionConfig.Voice != "" {
			speechConfig.VoiceConfig = &genai.VoiceConfig{
				PrebuiltVoiceConfig: &genai.PrebuiltVoiceConfig{
					VoiceName: g.sessionConfig.Voice,
				},
			}
			slog.Info("Using voice from session config",
				"event", "speech_config_voice",
				"voice", g.sessionConfig.Voice)
		}
	}

	// Connect to Gemini Live API
	session, err := g.client.Live.Connect(g.ctx, model, &genai.LiveConnectConfig{
		ResponseModalities: []genai.Modality{genai.ModalityAudio},
		SystemInstruction: &genai.Content{
			Parts: []*genai.Part{
				genai.NewPartFromText(systemInstructions),
			},
		},
		SpeechConfig:             speechConfig,
		InputAudioTranscription:  &genai.AudioTranscriptionConfig{},
		OutputAudioTranscription: &genai.AudioTranscriptionConfig{},
	})
	if err != nil {
		return fmt.Errorf("failed to connect to Gemini Live: %w", err)
	}

	g.session = session

	slog.Info("Successfully connected to Gemini Live session",
		"event", "gemini_connected",
		"participant", g.participantID)

	// Start receiving responses from Gemini
	g.wg.Add(1)
	go g.receiveResponses()

	return nil
}

// receiveResponses handles incoming responses from Gemini
func (g *GeminiHandler) receiveResponses() {
	defer g.wg.Done()

	slog.Info("Started receiving Gemini responses",
		"event", "gemini_recv_start",
		"participant", g.participantID)

	for {
		select {
		case <-g.stopChan:
			slog.Info("Stopped receiving Gemini responses",
				"event", "gemini_recv_stop",
				"participant", g.participantID)
			return

		default:
			// Check if session is already closed before attempting to receive
			g.sessionMu.RLock()
			sessionClosed := g.sessionClosed
			session := g.session
			g.sessionMu.RUnlock()

			if sessionClosed || session == nil {
				slog.Info("Session already closed, stopping receive loop",
					"event", "gemini_recv_closed",
					"participant", g.participantID)
				return
			}

			// Safely receive message with error recovery
			message, err := func() (*genai.LiveServerMessage, error) {
				defer func() {
					if r := recover(); r != nil {
						slog.Error("Recovered from panic in Gemini receive",
							"event", "gemini_recv_panic",
							"participant", g.participantID,
							"panic", fmt.Sprintf("%v", r))
					}
				}()
				return session.Receive()
			}()

			if err != nil {
				if err == io.EOF {
					slog.Info("Gemini session ended gracefully",
						"event", "gemini_recv_eof",
						"participant", g.participantID)
					g.markSessionClosed()
					g.removeFromBridge()
					return
				}
				slog.Error("Error receiving from Gemini, closing session",
					"event", "gemini_recv_error",
					"participant", g.participantID,
					"error", err.Error())
				// Mark session as closed to prevent further attempts
				g.markSessionClosed()
				g.removeFromBridge()
				return
			}

			if GEMINI_DEBUG {
				slog.Debug("Gemini message received",
					"event", "gemini_recv_msg",
					"participant", g.participantID,
					"message", fmt.Sprintf("%+v", message))
			}

			// Process server content (audio responses)
			if message.ServerContent != nil && message.ServerContent.ModelTurn != nil {
				for _, part := range message.ServerContent.ModelTurn.Parts {
					// Check if part has inline data (audio/video blob)
					if part.InlineData != nil {
						blob := part.InlineData

						// Audio data is already binary PCM, no base64 decoding needed
						audioData := blob.Data

						// Resample from 24000 Hz to 8000 Hz
						// Create a local resampler for this packet
						var outputBuf bytes.Buffer
						resampler, err := resample.New(&outputBuf, 24000.0, 8000.0, 1, resample.I16, resample.HighQ)
						if err != nil {
							slog.Error("Failed to create 24kHz->8kHz resampler",
								"event", "gemini_resample_error",
								"participant", g.participantID,
								"error", err.Error())
							continue
						}

						_, err = resampler.Write(audioData)
						if err != nil {
							slog.Error("Failed to write audio for resampling",
								"event", "gemini_resample_error",
								"participant", g.participantID,
								"error", err.Error())
							continue
						}

						// Close to flush the resampler buffer to outputBuf
						err = resampler.Close()
						if err != nil {
							slog.Error("Failed to resample audio",
								"event", "gemini_resample_error",
								"participant", g.participantID,
								"error", err.Error())
							continue
						}

						resampledAudio := outputBuf.Bytes()

						// Broadcast the full resampled audio buffer
						// The receiving participant (SIP) will handle chunking into RTP packets
						if err := g.BroadcastResponse(resampledAudio); err != nil {
							slog.Error("Error broadcasting Gemini audio",
								"event", "gemini_bcast_error",
								"participant", g.participantID,
								"error", err.Error())
							continue
						}

						slog.Info("Broadcasted Gemini audio",
							"event", "gemini_bcast_ok",
							"participant", g.participantID,
							"size_bytes", len(resampledAudio))
					}
				}
			}

			// Log transcription events
			if message.ServerContent != nil {
				if message.ServerContent.InputTranscription != nil {
					slog.Info("Gemini input transcription",
						"event", "gemini_input_transcription",
						"participant", g.participantID,
						"text", fmt.Sprintf("%+v", message.ServerContent.InputTranscription.Text))
				}
				if message.ServerContent.OutputTranscription != nil {
					slog.Info("Gemini output transcription",
						"event", "gemini_output_transcription",
						"participant", g.participantID,
						"text", fmt.Sprintf("%+v", message.ServerContent.OutputTranscription.Text))
				}
				if message.ServerContent.TurnComplete {
					slog.Info("Gemini turn completed",
						"event", "gemini_turn_complete",
						"participant", g.participantID)
				}

				// Check for interruption flag
				if message.ServerContent.Interrupted {
					slog.Info("Gemini interruption detected, flushing queues",
						"event", "gemini_interrupted",
						"participant", g.participantID)

					// Notify MediaBridge to flush queues for all participants
					if g.mediaBridge != nil {
						if err := g.mediaBridge.FlushQueues(); err != nil {
							slog.Error("Error flushing queues after interruption",
								"event", "gemini_interrupted_error",
								"participant", g.participantID,
								"error", err.Error())
						}
					}
				}

				// Log other non-audio message types (excluding transcriptions, turn complete, interrupted, and usage metadata)
				if message.ServerContent.ModelTurn == nil &&
				   message.ServerContent.InputTranscription == nil &&
				   message.ServerContent.OutputTranscription == nil &&
				   !message.ServerContent.TurnComplete &&
				   !message.ServerContent.Interrupted &&
				   message.UsageMetadata == nil {
					slog.Info("Non-audio Gemini message received",
						"event", "gemini_recv_msg",
						"participant", g.participantID,
						"content", fmt.Sprintf("%+v", message.ServerContent))
				}
			}
		}
	}
}

// markSessionClosed marks the session as closed to prevent further operations
func (g *GeminiHandler) markSessionClosed() {
	g.sessionMu.Lock()
	g.sessionClosed = true
	g.sessionMu.Unlock()
}

// removeFromBridge removes this participant from the media bridge
func (g *GeminiHandler) removeFromBridge() {
	if g.mediaBridge != nil {
		slog.Info("Removing participant from media bridge",
			"event", "gemini_bridge_remove",
			"participant", g.participantID)
		g.mediaBridge.RemoveParticipant(g.participantID)
	}
}

// BroadcastResponse broadcasts Gemini's audio response to other participants
func (g *GeminiHandler) BroadcastResponse(audioData []byte) error {
	// The audio data from Gemini is sent as a media chunk
	// The receiving end will handle any necessary formatting (e.g., RTP encapsulation)
	chunk := &MediaChunk{
		Data:     audioData,
		SenderID: g.participantID,
	}

	if err := g.mediaBridge.Broadcast(chunk); err != nil {
		if err != io.ErrClosedPipe {
			slog.Error("Error broadcasting Gemini response",
				"event", "gemini_bcast_error",
				"error", err.Error())
		}
		return err
	}

	return nil
}

// Close closes the Gemini handler
func (g *GeminiHandler) Close() error {
	slog.Info("Closing Gemini handler",
		"event", "gemini_handler_close",
		"participant", g.participantID)

	// Mark session as closed first to prevent any new operations
	g.markSessionClosed()

	// Stop processing
	select {
	case <-g.stopChan:
		// Already closed
	default:
		close(g.stopChan)
	}

	// Wait for receive goroutine to finish with timeout
	done := make(chan struct{})
	go func() {
		g.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		slog.Info("Gemini handler goroutines finished cleanly",
			"event", "gemini_goroutines_done",
			"participant", g.participantID)
	case <-time.After(5 * time.Second):
		slog.Warn("Timeout waiting for Gemini handler goroutines",
			"event", "gemini_goroutines_timeout",
			"participant", g.participantID)
	}

	// Close Gemini session safely
	g.sessionMu.Lock()
	session := g.session
	g.session = nil
	g.sessionMu.Unlock()

	if session != nil {
		if err := session.Close(); err != nil {
			slog.Error("Error closing Gemini session",
				"event", "gemini_session_close_error",
				"error", err.Error())
		}
	}

	// Note: genai.Client doesn't have a Close method, cleanup is handled via context cancellation

	// Cancel context
	if g.cancel != nil {
		g.cancel()
	}

	// Remove participant from media bridge
	if g.mediaBridge != nil {
		g.mediaBridge.RemoveParticipant(g.participantID)
	}

	slog.Info("Gemini handler closed",
		"event", "gemini_handler_closed",
		"participant", g.participantID)

	return nil
}
