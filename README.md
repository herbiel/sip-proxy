# SIP to Gemini Proxy

A lightweight SIP proxy server that bridges telephony systems with Google's Gemini Live API, enabling real-time voice AI conversations over traditional phone infrastructure.

## Overview

Gemini Live doesn't offer a native SIP interface for telephony integration—a common requirement for speech-to-speech models. While OpenAI includes this functionality out of the box, and Google suggests using platforms like LiveKit or Pipecat, those solutions add unnecessary complexity for simple telephony integration.

This proxy provides a straightforward solution: **SIP + RTP + G.711 → WebSockets + Raw PCM**

## Why This Proxy?

Traditional telephony uses well-established protocols:
- **SIP** (Session Initiation Protocol): Similar to HTTP but for telephony, using verbs like `INVITE` instead of `GET`/`POST`. Handles call setup and teardown over UDP or TCP.
- **RTP** (Real-time Transport Protocol): Binary protocol over UDP for low-overhead media transport, commonly using G.711 codecs (PCMU/PCMA).

On the other side:
- **Gemini Live API**: Uses a proprietary WebSocket protocol with PCM audio encoded in base64. While less efficient, it's perfectly acceptable for server-to-server communication.

This proxy handles all the protocol translation, audio resampling (8kHz ↔ 16kHz/24kHz), and codec conversion (G.711 ↔ PCM) automatically.

## Features

- **SIP Server**: Full SIP/RTP implementation supporting PCMU and PCMA (G.711) codecs
- **Gemini Integration**: Real-time bidirectional audio streaming with Gemini Live API
- **Twilio Ready**: Built-in webhook server for seamless Twilio integration
- **Flexible Configuration**: Support for custom system instructions, voice selection, and language settings via callback URL
- **Automatic Transcription**: Real-time transcription of both input and output audio
- **Audio Resampling**: Handles conversion between telephony (8kHz) and Gemini (16kHz/24kHz) sample rates
- **Codec Support**: G.711 μ-law (PCMU) and A-law (PCMA) codecs
- **Media Bridge**: N-way broadcasting architecture (extensible for conference scenarios)

## Architecture

```
Phone Call → Twilio → SIP Proxy → Gemini Live API
                ↓
         [SIP/RTP Server]
                ↓
         [Media Bridge]
                ↓
         [Gemini Handler]
```

The proxy consists of several key components:

- **SIP Server** ([sip.go](sip.go)): Handles SIP INVITE/BYE/ACK messages
- **RTP Handler** ([rtp.go](rtp.go)): Processes RTP packets and handles G.711 codec conversion
- **Media Bridge** ([bridge.go](bridge.go)): Routes audio between participants (SIP ↔ Gemini)
- **Gemini Handler** ([gemini.go](gemini.go)): Manages WebSocket connection to Gemini Live API
- **Twilio Server** ([twilio.go](twilio.go)): Webhook endpoint for Twilio integration

## Prerequisites

- **Go 1.24.0** or later
- **Google API Key** with Gemini API access
- **Twilio Account** (optional, for phone integration)

## Installation

### Clone the Repository

```bash
git clone <repository-url>
cd sip-proxy
```

### Install Dependencies

```bash
go mod download
```

### Build the Project

```bash
go build -o sip-proxy
```

## Configuration

### Environment Variables

Create a `.env` file in the project root (use [.env.example](.env.example) as a template):

```bash
# Required: Google API key for Gemini AI
GOOGLE_API_KEY=your-api-key-here

# Optional: Log output format (json or text)
# Default: json
# Use "text" for human-readable console output during development
# Use "json" for structured logging in production
LOG_FORMAT=json

# Optional: Log level (debug, info, warn, error)
# Default: debug
# Recommended for production: info
LOG_LEVEL=debug
```

### Command-Line Options

```bash
./sip-proxy [options]
```

**Optional:**
- `--callback-url`: HTTP callback URL for INVITE notifications (receives call details, returns agent configuration). If not provided, uses a default helpful assistant prompt.
- `--port`: SIP server port (default: 5060)
- `--twilio-port`: Twilio webhook server port (default: 8080)
- `--public-ip`: Public IP address (auto-detected if not specified)
- `--sip-url`: SIP URL for Twilio (defaults to `sip:PUBLIC_IP:5060`)
- `--sip-username`: SIP username for Twilio authentication
- `--sip-password`: SIP password for Twilio authentication
- `--twilio-from`: Caller ID for Twilio (default: +1123456789)

## Running the Proxy

### Basic Usage

With default prompt (no callback URL):
```bash
./sip-proxy
```

With custom callback URL:
```bash
./sip-proxy --callback-url=http://localhost:3000/callback
```

### With Custom Configuration

```bash
./sip-proxy \
  --callback-url=http://your-server.com/callback \
  --port=5060 \
  --twilio-port=8080 \
  --public-ip=1.2.3.4 \
  --sip-username=myuser \
  --sip-password=mypass \
  --twilio-from=+15551234567
```

### Development Mode (Human-Readable Logs)

```bash
LOG_FORMAT=text LOG_LEVEL=info ./sip-proxy
```

### Production Mode (JSON Logs)

```bash
LOG_FORMAT=json LOG_LEVEL=info ./sip-proxy
```

## Twilio Integration

### Setup Steps

1. **Get a Twilio Phone Number**
   Purchase a phone number from your Twilio console.

2. **Configure the Webhook**
   Point your Twilio number's voice webhook to your proxy:
   ```
   http://your-server.com:8080/
   ```

3. **Start the Proxy**
   ```bash
   ./sip-proxy
   ```
   Or with a custom callback URL:
   ```bash
   ./sip-proxy --callback-url=http://your-backend.com/callback
   ```

4. **Make a Call**
   Call your Twilio number and start talking to Gemini!

### Webhook Flow

1. User calls Twilio number
2. Twilio sends webhook request to proxy (port 8080)
3. Proxy returns TwiML with SIP dial instruction
4. Twilio establishes SIP/RTP connection with proxy (port 5060)
5. Proxy connects to Gemini Live API
6. Audio flows bidirectionally: Phone ↔ Proxy ↔ Gemini

## Callback URL API

The callback URL is **optional**. If not provided, the proxy uses a default configuration with a helpful assistant prompt.

When configured, the callback URL receives call information and returns configuration for the Gemini session.

### Request (POST)

```json
{
  "uri": "sip:+15551234567@your-server.com",
  "from": "sip:+15559876543@twilio.com",
  "call_id": "unique-call-id"
}
```

### Response (200 OK)

```json
{
  "system_instructions": "You are a helpful voice assistant. Be concise and friendly.",
  "voice": "Puck",
  "language": "en-US"
}
```

**Fields:**
- `system_instructions` (required): Instructions for Gemini's behavior
- `voice` (optional): Voice selection (e.g., "Puck", "Charon", "Kore", "Fenrir", "Aoede"). Default: "Puck"
- `language` (optional): Language code (e.g., "en-US", "es-ES"). Default: "en-US"

### Default Configuration

When no callback URL is provided, the proxy uses this default configuration:
```json
{
  "system_instructions": "You are a helpful voice assistant. Be concise, friendly, and natural in your responses. Keep your answers brief and conversational, as this is a phone call.",
  "voice": "Puck",
  "language": "en-US"
}
```

### Example Callback Server (Node.js)

```javascript
const express = require('express');
const app = express();

app.use(express.json());

app.post('/callback', (req, res) => {
  const { uri, from, call_id } = req.body;

  console.log(`Incoming call from ${from} to ${uri}`);

  res.json({
    system_instructions: "You are a helpful assistant. Keep responses brief and natural.",
    voice: "Puck",
    language: "en-US"
  });
});

app.listen(3000, () => {
  console.log('Callback server listening on port 3000');
});
```

## Audio Processing Details

### Codec Support
- **PCMU (G.711 μ-law)**: Payload type 0, most common in North America
- **PCMA (G.711 A-law)**: Payload type 8, common in Europe
- Automatic codec negotiation via SDP

### Sample Rate Conversion
- **SIP/RTP**: 8000 Hz (telephony standard)
- **Gemini Input**: 16000 Hz (upsampled from 8kHz)
- **Gemini Output**: 24000 Hz (downsampled to 8kHz)

### RTP Packetization
- 20ms packet duration (160 samples at 8kHz)
- Proper RTP timestamp and sequence number handling
- Packet queue for smooth transmission

## Logging

The proxy uses structured logging with configurable output:

### Log Events
- `sip_*`: SIP protocol events (INVITE, BYE, ACK)
- `rtp_*`: RTP packet send/receive events
- `gemini_*`: Gemini API interactions
- `twilio_*`: Twilio webhook events
- `media_bridge_*`: Media routing events

### Debug Modes
Enable detailed logging in [sip.go](sip.go:23-26):
```go
const (
    SIP_DEBUG    = true  // Log all SIP messages
    RTP_DEBUG    = false // Log all RTP packets (very verbose)
)
```

Enable Gemini message logging in [gemini.go](gemini.go:18-20):
```go
const (
    GEMINI_DEBUG = true  // Log all Gemini messages
)
```

## Development

### Key Dependencies

- `github.com/emiago/sipgo`: SIP protocol implementation
- `github.com/pion/rtp`: RTP packet handling
- `github.com/pion/sdp`: SDP parsing and generation
- `google.golang.org/genai`: Google Generative AI SDK
- `github.com/zaf/g711`: G.711 codec implementation
- `github.com/zaf/resample`: Audio resampling

### Testing

```bash
# Run tests
go test ./...

# Build with race detection
go build -race -o sip-proxy

# Run with debug logging
LOG_LEVEL=debug ./sip-proxy
```

## Troubleshooting

### Common Issues

**No audio from Gemini**
- Check that `GOOGLE_API_KEY` is set correctly
- Verify callback URL returns valid system instructions
- Enable `GEMINI_DEBUG` and check logs

**Call drops immediately**
- Verify public IP is correctly detected/configured
- Check firewall allows UDP on RTP ports (dynamic range)
- Enable `SIP_DEBUG` to see SIP message flow

**Poor audio quality**
- Check network jitter and packet loss
- Verify codec negotiation (check logs for "SDP offer codec support")
- Ensure proper sample rate conversion

**Twilio webhook fails**
- Confirm webhook URL is publicly accessible
- Check port 8080 is open and not blocked
- Verify TwiML response format in logs

## Performance Considerations

- **CPU**: Audio resampling is CPU-intensive; consider dedicated hardware for high call volumes
- **Memory**: Each session maintains audio buffers; ~10MB per concurrent call
- **Network**: UDP port range for RTP must be accessible; NAT traversal may require TURN server
- **Latency**: Typical end-to-end latency is 200-500ms depending on network conditions

## Security Notes

- **API Key Protection**: Never commit `.env` file; use secrets management in production
- **SIP Authentication**: Use `--sip-username` and `--sip-password` for Twilio authentication
- **Callback URL**: Validate requests in production to prevent unauthorized access
- **Network Security**: Restrict SIP port access to known Twilio IP ranges

## Limitations

- Single Gemini model supported: `gemini-live-2.5-flash-preview`
- No DTMF (touch-tone) support currently
- No call recording functionality
- No conference call support (architecture supports it, not implemented)

## License

[Add your license here]

## Contributing

[Add contribution guidelines here]

## Support

For issues and feature requests, please open a GitHub issue.

## Acknowledgments

Built with inspiration from the need to simplify Gemini Live API telephony integration, avoiding the complexity of full-featured platforms for simple use cases.
