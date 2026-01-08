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
		log.Println(".env file not found, using system environment variables instead")
	} else {
		log.Println(".env file loaded successfully")
	}
}

func GetEnv(variable string) (string, error) {
	var value = os.Getenv(variable)
	if value == "" {
		return "", fmt.Errorf("Missing variable '%s' in '.env'", variable)
	}
	return value, nil
}
