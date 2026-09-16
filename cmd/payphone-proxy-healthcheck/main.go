package main

import (
	"fmt"
	"net/http"
	"os"
	"time"
)

const healthEndpoint = "http://127.0.0.1:8080/healthz"

func main() {
	client := &http.Client{Timeout: 4 * time.Second}
	if err := check(client, healthEndpoint); err != nil {
		os.Exit(1)
	}
}

func check(client *http.Client, endpoint string) error {
	request, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("health endpoint returned %s", response.Status)
	}
	return nil
}
