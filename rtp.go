package main

import (
	"fmt"

	"github.com/pion/rtp"
	"github.com/zaf/g711"
)

// splitIntoChunks splits PCM audio data into chunks of specified duration
// sampleRate is in Hz, chunkDurationMs is in milliseconds
func splitIntoChunks(audioData []byte, sampleRate int, chunkDurationMs int) [][]byte {
	// Calculate bytes per chunk
	// For 16-bit (2 bytes) PCM: bytes = sampleRate * 2 * (duration_ms / 1000)
	bytesPerChunk := (sampleRate * 2 * chunkDurationMs) / 1000

	var chunks [][]byte
	for offset := 0; offset < len(audioData); offset += bytesPerChunk {
		end := offset + bytesPerChunk
		if end > len(audioData) {
			end = len(audioData)
		}
		chunk := make([]byte, end-offset)
		copy(chunk, audioData[offset:end])
		chunks = append(chunks, chunk)
	}

	return chunks
}

// decodeG711 converts G.711 (PCMU/PCMA) encoded audio to raw PCM
func decodeG711(rtpPayload []byte, codec string) []byte {
	if codec == "PCMU" {
		// Decode PCMU (μ-law) to 16-bit LPCM
		return g711.DecodeUlaw(rtpPayload)
	} else if codec == "PCMA" {
		// Decode PCMA (A-law) to 16-bit LPCM
		return g711.DecodeAlaw(rtpPayload)
	}
	// Unknown codec, return empty
	return []byte{}
}

// encodeG711 converts raw PCM (16-bit LPCM) to G.711 (PCMU/PCMA) encoded audio
func encodeG711(pcmData []byte, codec string) []byte {
	if codec == "PCMU" {
		// Encode 16-bit LPCM to PCMU (μ-law)
		return g711.EncodeUlaw(pcmData)
	} else if codec == "PCMA" {
		// Encode 16-bit LPCM to PCMA (A-law)
		return g711.EncodeAlaw(pcmData)
	}
	// Unknown codec, return empty
	return []byte{}
}

// extractRTPPayload extracts the payload and header info from an RTP packet using pion/rtp
func extractRTPPayload(rtpPacket []byte) (payloadType byte, payload []byte, sequenceNumber uint16, timestamp uint32, err error) {
	// Parse the RTP packet using pion/rtp
	var packet rtp.Packet
	if err := packet.Unmarshal(rtpPacket); err != nil {
		return 0, nil, 0, 0, fmt.Errorf("failed to unmarshal RTP packet: %w", err)
	}

	// Extract header information
	payloadType = packet.PayloadType
	sequenceNumber = packet.SequenceNumber
	timestamp = packet.Timestamp

	// Get the payload
	payload = packet.Payload

	// If no payload, return nil without error (normal for some packets)
	if len(payload) == 0 {
		return payloadType, nil, sequenceNumber, timestamp, nil
	}

	return payloadType, payload, sequenceNumber, timestamp, nil
}
