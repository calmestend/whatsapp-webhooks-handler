package main

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/calmestend/whatsapp-webhooks-handler/pkg/env"
)

func main() {
	env.Init()
	verifyToken, err := env.GetEnv("VERIFY_TOKEN")
	if err != nil {
		panic(fmt.Sprintf("Error getting VERIFY_TOKEN: %v", err))
	}

	portEnvVariable, err := env.GetEnv("PORT")
	if err != nil {
		panic(fmt.Sprintf("Error getting PORT: %v", err))
	}

	port, err := strconv.Atoi(portEnvVariable)
	if err != nil {
		panic(fmt.Sprintf("Error converting PORT to integer: %v", err))
	}

	http.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		mode := query.Get("hub.mode")
		challenge := query.Get("hub.challenge")
		token := query.Get("hub.verify_token")

		if mode == "subscribe" && token == verifyToken {
			log.Println("WEBHOOK VERIFIED")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(challenge))
		} else {
			w.WriteHeader(http.StatusForbidden)
		}
	})

	http.HandleFunc("POST /", func(w http.ResponseWriter, r *http.Request) {
		timestamp := time.Now().Format("2006-01-02 15:04:05")
		fmt.Printf("\nWebhook received %s\n", timestamp)

		body, err := io.ReadAll(r.Body)
		if err != nil {
			log.Printf("Error reading body: %v", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		defer r.Body.Close()

		fmt.Println(string(body))

		w.WriteHeader(http.StatusOK)
	})

	fmt.Printf("Server started at port %d\n", port)
	log.Fatal(http.ListenAndServe(fmt.Sprintf(":%d", port), nil))
}
