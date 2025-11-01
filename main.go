package main

import (
	"time"

	"github.com/bariiss/hls-proxy/cmd"
	log "github.com/sirupsen/logrus"
)

func init() {
	log.SetFormatter(&log.JSONFormatter{
		TimestampFormat: time.RFC3339Nano,
	})
}

func main() {
	if err := cmd.Execute(); err != nil {
		log.Fatal(err)
	}
}
