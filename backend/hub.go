package main

import (
	"log"
	"sync"
)

// Hub maintains the set of active clients and broadcasts messages to the clients.
type Hub struct {
	// Registered clients.
	clients map[*Client]bool

	// Inbound messages from the clients.
	// We don't directly use a broadcast channel in this setup for messages *from* clients to all *other* clients.
	// Instead, client messages are handled individually, and responses/updates are sent back to the originating client
	// or all clients if it's a general update (e.g., from a subscription).
	// For messages that need to be processed and potentially sent to chip-tool, they are handled by the client's readPump.

	// Register requests from the clients.
	register chan *Client

	// Unregister requests from clients.
	unregister chan *Client

	// Mutex to protect the clients map
	mu sync.Mutex

	// broadcastMessage is used if the hub itself needs to send a message to all clients
	// e.g. for a global notification or a shared log message initiated by the server.
	// For now, most messages are specific responses or logs per client.
	// broadcastMessage chan []byte
}

// NewHub creates a new Hub instance.
func NewHub() *Hub {
	return &Hub{
		register:   make(chan *Client),
		unregister: make(chan *Client),
		clients:    make(map[*Client]bool),
		// broadcastMessage: make(chan []byte), // If general broadcast needed
	}
}

// Run starts the hub's event loop.
func (h *Hub) Run() {
	for {
		select {
		case client := <-h.register:
			h.mu.Lock()
			h.clients[client] = true
			log.Printf("Client registered. Total clients: %d", len(h.clients))
			h.mu.Unlock()
		case client := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				close(client.send) // Close the client's send channel
				log.Printf("Client unregistered. Total clients: %d", len(h.clients))
			}
			h.mu.Unlock()
			// case message := <-h.broadcastMessage: // If general broadcast needed
			//  h.mu.Lock()
			//  for client := range h.clients {
			//      select {
			//      case client.send <- message:
			//      default:
			//          log.Printf("Client %v send channel full during broadcast, closing.", client.conn.RemoteAddr())
			//          close(client.send)
			//          delete(h.clients, client)
			//      }
			//  }
			//  h.mu.Unlock()
		}
	}
}

// sendToAllClients sends a message to all connected clients.
// Useful for global notifications or logs not tied to a specific client's request.
// Currently not used extensively as most communication is request/response per client.
/*
func (h *Hub) sendToAllClients(message []byte) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for client := range h.clients {
		select {
		case client.send <- message:
		default:
			// If the client's send buffer is full, assume it's slow or disconnected.
			log.Printf("Client %v send channel full, closing client.", client.conn.RemoteAddr())
			close(client.send)
			delete(h.clients, client)
		}
	}
}
*/
