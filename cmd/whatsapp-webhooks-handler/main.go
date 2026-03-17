package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/charmbracelet/log"
	"github.com/julienschmidt/httprouter"
)

var wg sync.WaitGroup
var verifyToken string
var backendURL string
var portEnv string

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
		wrapped := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}

		next(wrapped, r)

		log.Info("request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", wrapped.statusCode,
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

	flag.StringVar(&verifyToken, "verify_token", os.Getenv("WHATSAPP_WEBHOOK_VERIFY_TOKEN"), "Verify Token")
	flag.StringVar(&backendURL, "backend_url", os.Getenv("WHATSAPP_WEBHOOK_BACKEND_URL"), "Backend URL")
	flag.StringVar(&portEnv, "port", os.Getenv("WHATSAPP_WEBHOOK_PORT"), "Server Port")

	if verifyToken == "" || backendURL == "" || portEnv == "" {
		log.Fatal("error getting env variables")
	}

	port, err := strconv.Atoi(portEnv)
	if err != nil {
		log.Fatal("error getting env var", "var", "WHATSAPP_WEBHOOK_PORT", "err", err)
	}

	flag.Parse()

	log.Info("server started", "port", port)
	if err := serve(port); err != nil {
		log.Fatal("server error", "err", err)
	}
}

func subscribe(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	mode := query.Get("hub.mode")
	challenge := query.Get("hub.challenge")
	token := query.Get("hub.verify_token")

	if mode == "subscribe" && token == verifyToken {
		log.Info("webhook verified")
		w.WriteHeader(http.StatusOK)
		if _, err := w.Write([]byte(challenge)); err != nil {
			log.Error("error writting header", "err", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
	} else {
		log.Warn("webhook verification failed", "token", token, "mode", mode)
		w.WriteHeader(http.StatusForbidden)
	}
}

func forwarding(w http.ResponseWriter, r *http.Request) {
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
}

func routes() http.Handler {
	router := httprouter.New()

	router.HandlerFunc(http.MethodGet , "/webhook/v1", loggerMiddleware(subscribe))
	router.HandlerFunc(http.MethodPost, "/webhook/v1", loggerMiddleware(forwarding))

	return router
}

func serve(port int) error {
	srv := &http.Server{
		Addr:         fmt.Sprintf(":%d", port),
		Handler:      routes(),
		IdleTimeout:  time.Minute,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	shutdownError := make(chan error)

	go func() {
		quit := make(chan os.Signal, 1)
		signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

		s := <-quit
		log.Info("shutting down server", "signal", s.String())

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		err := srv.Shutdown(ctx)
		if err != nil {
			shutdownError <- srv.Shutdown(ctx)
		}

		log.Info("completing background tasks", "addr", srv.Addr)
		wg.Wait()
		shutdownError <- nil
	}()

	log.Info("starting server", "addr", srv.Addr)

	err := srv.ListenAndServe()
	if !errors.Is(err, http.ErrServerClosed) {
		return err
	}

	err = <-shutdownError
	if err != nil {
		return err
	}

	log.Info("stopped server", "addr", srv.Addr)
	return nil
}
