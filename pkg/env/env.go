package env

import (
	"fmt"
	"log"
	"os"

	"github.com/joho/godotenv"
)

func Init() {
	err := godotenv.Load()
	if err != nil {
		log.Fatal("Error loading .env file")
	}
}

func GetEnv(variable string) (string, error) {
	var value = os.Getenv(variable)
	if value == "" {
		return "", fmt.Errorf("Missing variable '%s' in '.env'", variable)
	}
	return value, nil
}
