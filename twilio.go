package main

import (
	"fmt"
	"log/slog"
	"net/http"
)

// TwilioServer handles Twilio webhook requests
type TwilioServer struct {
	sipUsername string
	sipPassword string
	sipURL      string
	port        int
	from        string
}

// NewTwilioServer creates a new Twilio webhook server
func NewTwilioServer(port int, sipUsername, sipPassword, sipURL, from string) *TwilioServer {
	return &TwilioServer{
		sipUsername: sipUsername,
		sipPassword: sipPassword,
		sipURL:      sipURL,
		port:        port,
		from:        from,
	}
}

// handleWebhook processes POST requests and returns TwiML
func (t *TwilioServer) handleWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		slog.Warn("Received non-POST request to Twilio webhook",
			"event", "twilio_invalid_method",
			"method", r.Method)
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	slog.Info("Received Twilio webhook request",
		"event", "twilio_webhook",
		"remote_addr", r.RemoteAddr)

	// Generate TwiML response
	twimlResponse := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<Response>
  <Dial callerId="%s">
    <Sip username="%s" password="%s">%s</Sip>
  </Dial>
</Response>`, t.from, t.sipUsername, t.sipPassword, t.sipURL)

	// Set content type to XML
	w.Header().Set("Content-Type", "text/xml")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(twimlResponse))

	slog.Info("Sent TwiML response",
		"event", "twilio_response",
		"sip_url", t.sipURL)
}

// Start begins listening for webhook requests
func (t *TwilioServer) Start() error {
	http.HandleFunc("/", t.handleWebhook)

	addr := fmt.Sprintf(":%d", t.port)
	slog.Info("Starting Twilio webhook server",
		"event", "twilio_server_start",
		"port", t.port)

	go func() {
		if err := http.ListenAndServe(addr, nil); err != nil {
			slog.Error("Twilio server error",
				"event", "twilio_server_error",
				"error", err.Error())
		}
	}()

	return nil
}
