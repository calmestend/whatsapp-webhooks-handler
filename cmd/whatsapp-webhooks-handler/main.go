package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/charmbracelet/log"
)

type WebhookPayload struct {
	Entry []Entry `json:"entry"`
}

type Entry struct {
	ID      string   `json:"id"`
	Changes []Change `json:"changes"`
}

type Change struct {
	Value ChangeValue `json:"value"`
	Field string      `json:"field"`
}

type ChangeValue struct {
	Event    string   `json:"event"`
	WABAInfo WABAInfo `json:"waba_info"`
}

type WABAInfo struct {
	WABAId          string `json:"waba_id"`
	OwnerBusinessId string `json:"owner_business_id"`
	PartnerAppId    string `json:"partner_app_id"`
}

type BackendPayload struct {
	WABAId          string `json:"waba_id"`
	OwnerBusinessId string `json:"owner_business_id"`
	PartnerAppId    string `json:"partner_app_id"`
}

func loggerMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		wrapped := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}

		next(wrapped, r)

		log.Info("request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", wrapped.statusCode,
			"duration", time.Since(start),
			"ip", r.RemoteAddr,
		)
	}
}

type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

func forwardToBackend(backendUrl string, payload BackendPayload) error {
	jsonData, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("error marshaling payload: %w", err)
	}

	resp, err := http.Post(backendUrl, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("error posting to backend: %w", err)
	}
	defer resp.Body.Close()

	log.Info("backend response", "status", resp.StatusCode)
	return nil
}

func main() {
	log.SetTimeFormat(time.RFC3339)
	log.SetReportCaller(true)


	var verifyToken string
	var backendURL string
	var portEnv string

	flag.StringVar(&verifyToken, "verify_token", os.Getenv("WHATSAPP_WEBHOOK_VERIFY_TOKEN"), "Verify Token")
	flag.StringVar(&backendURL, "backend_url", os.Getenv("WHATSAPP_WEBHOOK_BACKEND_URL"), "Backend URL")
	flag.StringVar(&portEnv, "port", os.Getenv("WHATSAPP_WEBHOOK_PORT"), "Server Port")

	port, err := strconv.Atoi(portEnv)
	if err != nil {
		log.Fatal("error getting env var", "var", "WHATSAPP_WEBHOOK_PORT", "err", err)
	}

	flag.Parse()

	http.HandleFunc("GET /", loggerMiddleware(func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		mode := query.Get("hub.mode")
		challenge := query.Get("hub.challenge")
		token := query.Get("hub.verify_token")

		if mode == "subscribe" && token == verifyToken {
			log.Info("webhook verified")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(challenge))
		} else {
			log.Warn("webhook verification failed", "token", token, "mode", mode)
			w.WriteHeader(http.StatusForbidden)
		}
	}))

	http.HandleFunc("POST /", loggerMiddleware(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			log.Error("error reading body", "err", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		defer r.Body.Close()

		log.Debug("webhook payload received", "body", string(body))

		var payload WebhookPayload
		if err := json.Unmarshal(body, &payload); err != nil {
			log.Error("error parsing body", "err", err)
			w.WriteHeader(http.StatusOK)
			return
		}

		for _, entry := range payload.Entry {
			for _, change := range entry.Changes {
				if change.Value.Event == "PARTNER_APP_INSTALLED" {
					log.Info("partner app installed",
						"waba_id", change.Value.WABAInfo.WABAId,
						"owner_business_id", change.Value.WABAInfo.OwnerBusinessId,
						"partner_app_id", change.Value.WABAInfo.PartnerAppId,
					)

					err := forwardToBackend(backendURL, BackendPayload{
						WABAId:          change.Value.WABAInfo.WABAId,
						OwnerBusinessId: change.Value.WABAInfo.OwnerBusinessId,
						PartnerAppId:    change.Value.WABAInfo.PartnerAppId,
					})
					if err != nil {
						log.Error("error forwarding to backend", "err", err)
					}
				}
			}
		}

		w.WriteHeader(http.StatusOK)
	}))

	log.Info("server started", "port", port)
	if err := http.ListenAndServe(fmt.Sprintf(":%d", port), nil); err != nil {
		log.Fatal("server error", "err", err)
	}
}
