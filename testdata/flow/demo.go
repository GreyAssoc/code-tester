package flowdemo

import (
	"fmt"
	"os"
)

type Config struct {
	Token string
	Name  string
}

var cfg Config

func LoadSecret() string {
	t := os.Getenv("API_TOKEN")
	cfg.Token = t
	return t
}

func LogToken() {
	_ = LoadSecret()
	fmt.Println(cfg.Token) // secret field reaches logger
}

func Dropped(userInput string) {
	_ = userInput
}

func UseConfig() string {
	return cfg.Name
}
