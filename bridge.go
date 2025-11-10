package main

import (
	"io"
	"log/slog"
	"sync"
)

// MediaChunk represents a media payload (e.g., PCM audio) with sender information
type MediaChunk struct {
	Data     []byte
	SenderID string
}

// Participant represents a participant that can send and receive RTP packets
type Participant interface {
	// ID returns unique identifier for this participant
	ID() string

	// Writer returns the io.Writer to send RTP packets to this participant
	Writer() io.Writer
}

// QueueFlusher is an optional interface that participants can implement
// to support flushing their output queues (e.g., on interruption)
type QueueFlusher interface {
	FlushQueue()
}

// MediaBridge handles N-way media broadcasting
type MediaBridge interface {
	// AddParticipant adds a new participant to the bridge
	AddParticipant(participant Participant) error

	// RemoveParticipant removes a participant from the bridge
	RemoveParticipant(participantID string) error

	// Broadcast sends a media chunk to all participants except the sender
	Broadcast(chunk *MediaChunk) error

	// FlushQueues notifies all participants that implement QueueFlusher to flush their queues
	FlushQueues() error

	// Start begins processing packets
	Start() error

	// Stop closes the bridge
	Stop() error
}

// DefaultMediaBridge implements MediaBridge for N-way broadcasting
type DefaultMediaBridge struct {
	participants map[string]Participant
	chunkChan    chan *MediaChunk
	stopChan     chan struct{}
	wg           sync.WaitGroup
	mu           sync.RWMutex
}

// NewMediaBridge creates a new media bridge
func NewMediaBridge() *DefaultMediaBridge {
	return &DefaultMediaBridge{
		participants: make(map[string]Participant),
		chunkChan:    make(chan *MediaChunk, 200),
		stopChan:     make(chan struct{}),
	}
}

// AddParticipant adds a new participant to the bridge
func (m *DefaultMediaBridge) AddParticipant(participant Participant) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	id := participant.ID()
	m.participants[id] = participant
	slog.Info("Added participant to media bridge",
		"event", "participant_added",
		"participant", id,
		"total", len(m.participants))

	return nil
}

// RemoveParticipant removes a participant from the bridge
func (m *DefaultMediaBridge) RemoveParticipant(participantID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.participants[participantID]; exists {
		delete(m.participants, participantID)
		slog.Info("Removed participant from media bridge",
			"event", "participant_removed",
			"participant", participantID,
			"total", len(m.participants))
	}

	return nil
}

// Broadcast sends a media chunk to all participants except the sender
func (m *DefaultMediaBridge) Broadcast(chunk *MediaChunk) error {
	select {
	case m.chunkChan <- chunk:
		return nil
	case <-m.stopChan:
		return io.ErrClosedPipe
	default:
		// Channel full, drop chunk
		slog.Warn("Media chunk dropped, channel full",
			"event", "chunk_dropped",
			"sender", chunk.SenderID)
		return nil
	}
}

// FlushQueues notifies all participants that implement QueueFlusher to flush their queues
func (m *DefaultMediaBridge) FlushQueues() error {
	m.mu.RLock()
	participantsSnapshot := make(map[string]Participant, len(m.participants))
	for id, p := range m.participants {
		participantsSnapshot[id] = p
	}
	m.mu.RUnlock()

	flushedCount := 0
	for id, participant := range participantsSnapshot {
		// Check if participant implements QueueFlusher interface
		if flusher, ok := participant.(QueueFlusher); ok {
			flusher.FlushQueue()
			flushedCount++
			slog.Info("Flushed queue for participant",
				"event", "mediabridge_flush",
				"participant", id)
		}
	}

	slog.Info("Flushed queues for participants",
		"event", "mediabridge_flush_complete",
		"flushed_count", flushedCount,
		"total_participants", len(participantsSnapshot))
	return nil
}

// Start begins processing media chunks
func (m *DefaultMediaBridge) Start() error {
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		for {
			select {
			case chunk := <-m.chunkChan:
				m.broadcastChunk(chunk)

			case <-m.stopChan:
				return
			}
		}
	}()

	slog.Info("Media bridge started",
		"event", "media_bridge_started")
	return nil
}

// broadcastChunk sends chunk to all participants except the sender
func (m *DefaultMediaBridge) broadcastChunk(chunk *MediaChunk) {
	m.mu.RLock()
	participantsSnapshot := make(map[string]Participant, len(m.participants))
	for id, p := range m.participants {
		participantsSnapshot[id] = p
	}
	m.mu.RUnlock()

	sentCount := 0
	var participantsToRemove []string

	for id, participant := range participantsSnapshot {
		// Skip the sender
		if id == chunk.SenderID {
			continue
		}

		writer := participant.Writer()
		if writer != nil {
			if _, err := writer.Write(chunk.Data); err != nil {
				// If it's a terminal error (closed pipe), mark for removal
				if err == io.ErrClosedPipe {
					slog.Info("Participant closed, will be removed from bridge",
						"event", "participant_closed",
						"participant", id)
					participantsToRemove = append(participantsToRemove, id)
				} else {
					slog.Error("Error writing to participant",
						"event", "write_error",
						"participant", id,
						"error", err.Error())
				}
			} else {
				sentCount++
			}
		}
	}

	// Remove participants that returned ErrClosedPipe
	if len(participantsToRemove) > 0 {
		m.mu.Lock()
		for _, id := range participantsToRemove {
			delete(m.participants, id)
			slog.Info("Auto-removed closed participant",
				"event", "participant_auto_removed",
				"participant", id,
				"total", len(m.participants))
		}
		m.mu.Unlock()
	}

	// Commented out for performance - uncomment if debugging broadcasts
	// if sentCount > 0 {
	// 	slog.Debug("Broadcasted chunk to participants",
	// 		"event", "chunk_broadcasted",
	// 		"sender", chunk.SenderID,
	// 		"recipients", sentCount)
	// }
}

// Stop closes the bridge
func (m *DefaultMediaBridge) Stop() error {
	close(m.stopChan)
	m.wg.Wait()

	m.mu.Lock()
	m.participants = make(map[string]Participant)
	m.mu.Unlock()

	slog.Info("Media bridge stopped",
		"event", "media_bridge_stopped")
	return nil
}

// BaseParticipant provides a basic implementation of Participant
type BaseParticipant struct {
	id     string
	writer io.Writer
}

// NewParticipant creates a new base participant
func NewParticipant(id string, writer io.Writer) *BaseParticipant {
	return &BaseParticipant{
		id:     id,
		writer: writer,
	}
}

// ID returns the participant's unique identifier
func (p *BaseParticipant) ID() string {
	return p.id
}

// Writer returns the io.Writer for this participant
func (p *BaseParticipant) Writer() io.Writer {
	return p.writer
}

// SetWriter updates the writer for this participant
func (p *BaseParticipant) SetWriter(writer io.Writer) {
	p.writer = writer
}
