package main

import (
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/gorilla/websocket"
)

func main() {
	if len(os.Args) < 3 {
		log.Fatal("Usage: go run test_ws_manual.go <script-id> <api-key>")
	}
	scriptID := os.Args[1]
	apiKey := os.Args[2]

	u := url.URL{Scheme: "ws", Host: "localhost:18083", Path: "/api/scripts/" + scriptID + "/terminal", RawQuery: "key=" + apiKey}
	log.Printf("Connecting to %s", u.String())

	c, resp, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		if resp != nil {
			log.Fatalf("Dial failed: %v, status: %s", err, resp.Status)
		}
		log.Fatalf("Dial failed: %v", err)
	}
	defer c.Close()

	log.Println("Connected! Waiting for history logs...")

	// 设置 5 秒超时，如果没有收到任何消息则退出
	c.SetReadDeadline(time.Now().Add(5 * time.Second))

	messageCount := 0
	for {
		mt, message, err := c.ReadMessage()
		if err != nil {
			log.Printf("Finished reading: %v", err)
			break
		}
		messageCount++
		if mt == websocket.BinaryMessage {
			fmt.Printf("\n--- Received Binary Message (Length: %d) ---\n%s\n--------------------------------------------\n", len(message), string(message))
		} else {
			fmt.Printf("\n--- Received Text Message ---\n%s\n---------------------------\n", string(message))
		}
	}

	if messageCount == 0 {
		log.Println("FAILED: No messages received from WebSocket.")
	} else {
		log.Printf("SUCCESS: Received %d messages.", messageCount)
	}
}
