package main

import (
	"flag"
	"log"
	"net/http"
	"os/exec"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
)

var addr = flag.String("addr", ":8080", "http service address for the backend")

func main() {
	flag.Parse()
	log.SetFlags(log.LstdFlags | log.Lshortfile) // Add file and line number to logs

	// Check if chip-tool is accessible (basic check)
	// This doesn't guarantee it works, but checks if the command exists.
	cmd := exec.Command(chipToolPath, "--version")
	if err := cmd.Run(); err != nil {
		log.Printf("WARNING: chip-tool command '%s' not found or not executable. Please ensure it's installed and in PATH, or chipToolPath is set correctly in handlers.go. Error: %v", chipToolPath, err)
		log.Println("The backend might not function correctly for Matter device interactions.")
		// os.Exit(1) // Optionally exit if chip-tool is critical and not found.
	} else {
		log.Printf("chip-tool found at '%s' and seems executable.", chipToolPath)
	}


	hub := NewHub()
	go hub.Run() // Start the WebSocket hub in a separate goroutine

	router := gin.New() // Use gin.New() for more control over middleware
	router.Use(gin.Logger())   // Gin's default logger
	router.Use(gin.Recovery()) // Gin's default recovery middleware

	// Configure CORS
	// The frontend runs on http://localhost:5173 (default Vite port)
	// The backend runs on http://<rpi_ip>:8080
	config := cors.DefaultConfig()
	// Allow specific origins. For development, localhost for Vue and potentially RPi's IP if accessing directly.
	// For production, replace with your frontend's actual domain.
	config.AllowOrigins = []string{"http://localhost:5173", "http://127.0.0.1:5173"} 
	// If accessing frontend from another machine on the network, you might need to add that origin too,
	// or allow all origins for wider testing (config.AllowAllOrigins = true), but be cautious.
	// config.AllowAllOrigins = true // For easier testing, but less secure for production
	config.AllowMethods = []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"}
	config.AllowHeaders = []string{"Origin", "Content-Length", "Content-Type", "Authorization"}
	config.AllowCredentials = true // Important for WebSocket if it ever needs credentials/cookies

	router.Use(cors.New(config))

	// WebSocket endpoint
	router.GET("/ws", func(c *gin.Context) {
		serveWs(hub, c.Writer, c.Request)
	})

	// Example REST endpoint (optional, if needed for non-realtime tasks or health checks)
	router.GET("/api/status", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"status":          "Matter Backend Running",
			"websocket_clients": len(hub.clients), // Example of exposing some hub info
		})
	})

	log.Printf("Matter Backend Server starting on %s", *addr)
	if err := router.Run(*addr); err != nil {
		log.Fatalf("Failed to run server: %v", err)
	}
}
