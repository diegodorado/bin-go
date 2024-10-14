package main

import (
	"fmt"
	"log"
	"net/http"
	"sync"

	"github.com/gorilla/websocket"
)

type Message struct {
	Type    string `json:"type"` // "pub", "sub", or "unsub"
	Topic   string `json:"topic"`
	Content string `json:"content"`
}

type Client struct {
	conn   *websocket.Conn
	topics map[string]bool
	mu     sync.Mutex
}

var (
	clients    = make(map[*Client]bool)
	broadcast  = make(chan Message)
	register   = make(chan *Client)
	unregister = make(chan *Client)
	mutex      = &sync.Mutex{}
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true // Be cautious with this in production
	},
}

func handleConnections(w http.ResponseWriter, r *http.Request) {
	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Fatal(err)
	}

	client := &Client{conn: ws, topics: make(map[string]bool)}
	register <- client

	defer func() {
		unregister <- client
		ws.Close()
	}()

	for {
		var msg Message
		err := ws.ReadJSON(&msg)
		if err != nil {
			log.Printf("error: %v", err)
			break
		}

		client.mu.Lock()
		switch msg.Type {
		case "sub":
			client.topics[msg.Topic] = true
		case "unsub":
			delete(client.topics, msg.Topic)
		case "pub":
			broadcast <- msg
		}
		client.mu.Unlock()
	}

}

func handleMessages() {
	for {
		select {
		case client := <-register:
			addClient(client)
		case client := <-unregister:
			removeClient(client)
		case message := <-broadcast:
			broadcastMessage(message)
		}
	}
}

func addClient(client *Client) {
	mutex.Lock()
	defer mutex.Unlock()
	clients[client] = true
}

func removeClient(client *Client) {
	mutex.Lock()
	defer mutex.Unlock()
	if _, ok := clients[client]; ok {
		delete(clients, client)
		client.conn.Close()
	}
}

func broadcastMessage(message Message) {
	mutex.Lock()
	defer mutex.Unlock()

	for client := range clients {
		go func(c *Client) {
			c.mu.Lock()
			defer c.mu.Unlock()

			if c.topics[message.Topic] {
				err := c.conn.WriteJSON(message)
				if err != nil {
					log.Printf("error: %v", err)
					c.conn.Close()
					removeClient(c)
				}
			}
		}(client)
	}
}

func main() {

	http.HandleFunc("/", handleConnections)

	go handleMessages()

	err := http.ListenAndServe(":8080", nil)
	if err != nil {
		fmt.Println("ListenAndServe:", err)
	}
}
