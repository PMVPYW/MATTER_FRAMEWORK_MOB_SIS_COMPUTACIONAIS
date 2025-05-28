package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const (
	// chipToolPath should be the command to run chip-tool.
	// If it's in PATH: "chip-tool"
	// If installed via snap: "/snap/bin/chip-tool" or "matter-pi-tool.chip-tool"
	// If built from source: path to your compiled chip-tool executable, e.g., "/home/pi/connectedhomeip/out/chip-tool-arm64/chip-tool"
	chipToolPath = "chip-tool" // IMPORTANT: Verify this path on your RPi

	// paaTrustStorePath might be needed for commissioning production devices.
	// Example: "/path/to/connectedhomeip/credentials/production/paa-root-certs/"
	// For testing with non-production devices, this might not be strictly necessary or can be omitted.
	// paaTrustStorePath = "/home/pi/connectedhomeip/credentials/development/paa-root-certs" // Adjust if needed
)

// WebSocket upgrader
var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		// Allow all connections for development.
		// For production, you should validate the origin:
		// origin := r.Header.Get("Origin")
		// return origin == "http://localhost:5173" // Your Vue3 dev server
		return true
	},
}

// Constants for WebSocket handling
const (
	writeWait      = 10 * time.Second    // Time allowed to write a message to the peer.
	pongWait       = 60 * time.Second    // Time allowed to read the next pong message from the peer.
	pingPeriod     = (pongWait * 9) / 10 // Send pings to peer with this period. Must be less than pongWait.
	maxMessageSize = 1024 * 10           // Maximum message size allowed from peer.
)

// Client is a middleman between the WebSocket connection and the hub.
type Client struct {
	hub *Hub
	// The WebSocket connection.
	conn *websocket.Conn
	// Buffered channel of outbound messages.
	send chan []byte
	// Mutex to protect concurrent writes to the WebSocket connection
	writeMu sync.Mutex
}

type SubscribeAttributePayload struct {
	NodeID      string `json:"nodeId"`
	EndpointID  string `json:"endpointId"` // Default to "1" if not provided by client
	Cluster     string `json:"cluster"`
	Attribute   string `json:"attribute"`
	MinInterval string `json:"minInterval"` // In seconds, e.g., "1"
	MaxInterval string `json:"maxInterval"` // In seconds, e.g., "10"
}

// readPump pumps messages from the WebSocket connection to the hub.
// The hub calls this method for each registered client.
func (c *Client) readPump() {
	defer func() {
		c.hub.unregister <- c
		c.conn.Close()
		log.Printf("Client %v disconnected from readPump", c.conn.RemoteAddr())
	}()
	c.conn.SetReadLimit(maxMessageSize)
	_ = c.conn.SetReadDeadline(time.Now().Add(pongWait)) // Initial read deadline
	c.conn.SetPongHandler(func(string) error {
		_ = c.conn.SetReadDeadline(time.Now().Add(pongWait)) // Reset read deadline on pong
		return nil
	})

	for {
		_, messageBytes, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure, websocket.CloseNormalClosure) {
				log.Printf("Client %v read error: %v", c.conn.RemoteAddr(), err)
			} else {
				log.Printf("Client %v WebSocket closed: %v", c.conn.RemoteAddr(), err)
			}
			break
		}

		var clientMsg ClientMessage
		if err := json.Unmarshal(messageBytes, &clientMsg); err != nil {
			log.Printf("Error unmarshalling client message from %v: %v. Message: %s", c.conn.RemoteAddr(), err, string(messageBytes))
			c.notifyClient("error", map[string]interface{}{"message": "Invalid message format: " + err.Error()})
			continue
		}

		log.Printf("Received message from client %v: Type: %s, Payload: %+v", c.conn.RemoteAddr(), clientMsg.Type, clientMsg.Payload)
		go handleClientMessage(c, clientMsg) // Handle each message in a new goroutine
	}
}

// writePump pumps messages from the hub to the WebSocket connection.
func (c *Client) writePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.conn.Close() // Ensure connection is closed if writePump exits
		log.Printf("Client %v disconnected from writePump", c.conn.RemoteAddr())
		// The readPump's defer will handle unregistering from the hub.
	}()
	for {
		select {
		case message, ok := <-c.send:
			c.writeMu.Lock()
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				// The hub closed the channel.
				log.Printf("Client %v send channel closed, sending close message.", c.conn.RemoteAddr())
				_ = c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				c.writeMu.Unlock()
				return
			}

			w, err := c.conn.NextWriter(websocket.TextMessage)
			if err != nil {
				log.Printf("Client %v error getting next writer: %v", c.conn.RemoteAddr(), err)
				c.writeMu.Unlock()
				return // Exit if we can't get a writer
			}
			_, err = w.Write(message)
			if err != nil {
				log.Printf("Client %v error writing message: %v", c.conn.RemoteAddr(), err)
				// Attempt to close writer even on error
				_ = w.Close()
				c.writeMu.Unlock()
				return // Exit on write error
			}

			// Add queued messages to the current WebSocket message.
			// This can improve efficiency by batching messages.
			n := len(c.send)
			for i := 0; i < n; i++ {
				_, _ = w.Write([]byte{'\n'}) // Optional: newline separator for multiple JSON objects
				_, err = w.Write(<-c.send)
				if err != nil {
					log.Printf("Client %v error writing queued message: %v", c.conn.RemoteAddr(), err)
					_ = w.Close()
					c.writeMu.Unlock()
					return
				}
			}

			if err := w.Close(); err != nil {
				log.Printf("Client %v error closing writer: %v", c.conn.RemoteAddr(), err)
				c.writeMu.Unlock()
				return // Exit on close error
			}
			c.writeMu.Unlock()

		case <-ticker.C:
			c.writeMu.Lock()
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				log.Printf("Client %v error sending ping: %v", c.conn.RemoteAddr(), err)
				c.writeMu.Unlock()
				return // Exit if ping fails
			}
			c.writeMu.Unlock()
		}
	}
}

// serveWs handles WebSocket requests from the peer.
func serveWs(hub *Hub, w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println("WebSocket upgrade error:", err)
		return
	}
	client := &Client{hub: hub, conn: conn, send: make(chan []byte, 256)}
	client.hub.register <- client

	log.Printf("Client %v connected via WebSocket", conn.RemoteAddr())

	// Allow collection of memory referenced by the caller by doing all work in new goroutines.
	go client.writePump()
	go client.readPump()
}

// handleClientMessage processes messages from the client and interacts with chip-tool.
func handleClientMessage(client *Client, msg ClientMessage) {
	switch msg.Type {
	case "discover_devices":
		log.Println("Handling discover_devices request")
		client.notifyClientLog("discovery_log", "Starting BLE device discovery via chip-tool...")

		// chip-tool discover ble --timeout 10000 (10 seconds)
		// Note: `chip-tool discover ble` might require sudo or specific permissions.
		cmd := exec.Command(chipToolPath, "discover", "commissionables")
		
		var outBuf, errBuf bytes.Buffer
		cmd.Stdout = &outBuf
		cmd.Stderr = &errBuf

		err := cmd.Run()
		stdout := outBuf.String()
		stderr := errBuf.String()

		log.Printf("chip-tool discover stdout:\n%s", stdout)
		if stderr != "" {
			log.Printf("chip-tool discover stderr:\n%s", stderr)
		}
		
		if err != nil {
			errMsg := fmt.Sprintf("Error running chip-tool discover: %v. Stderr: %s", err, stderr)
			log.Println(errMsg)
			client.notifyClientLog("discovery_log", "Error during discovery: "+errMsg)
			client.sendPayload("discovery_result", DiscoveryResultPayload{Devices: []DiscoveredDevice{}, Error: errMsg})
			return
		}
		
		client.notifyClientLog("discovery_log", "Discovery command finished. Output:\n"+stdout)
		discovered := parseDiscoveryOutput(stdout, client) // Pass client for logging within parser
		client.sendPayload("discovery_result", DiscoveryResultPayload{Devices: discovered})

	case "commission_device":
		var payload CommissionDevicePayload
		payloadBytes, _ := json.Marshal(msg.Payload) // Convert interface{} to map[string]interface{} then to struct
		if err := json.Unmarshal(payloadBytes, &payload); err != nil {
			client.notifyClientLog("commissioning_log", "Invalid payload for commission_device: "+err.Error())
			client.sendPayload("commissioning_status", CommissioningStatusPayload{Success: false, Error: "Invalid payload: " + err.Error()})
			return
		}

		log.Printf("Handling commission_device request: %+v", payload)
		if payload.SetupCode == "" || payload.NodeIDToAssign == "" || payload.Discriminator == "" {
			client.notifyClientLog("commissioning_log", "Missing discriminator, setupCode, or nodeIdToAssign for commissioning.")
			client.sendPayload("commissioning_status", CommissioningStatusPayload{Success: false, Error: "Missing discriminator, setupCode, or nodeIdToAssign.", OriginalDiscriminator: payload.Discriminator})
			return
		}

		client.notifyClientLog("commissioning_log", fmt.Sprintf("Starting commissioning for Discriminator %s, proposed Node ID %s with setup code %s", payload.Discriminator, payload.NodeIDToAssign, payload.SetupCode))

		// chip-tool pairing ble-thread <node-id-to-assign> <setup-payload> <discriminator> <operational-dataset>
		// or chip-tool pairing ble-wifi <node-id-to-assign> <setup-payload> <discriminator> <wifi-ssid> <wifi-password>
		// For simplicity, using `code` which assumes device is already on an IP network or for simpler BLE cases.
		// A more robust solution needs to determine commissioning method (ble-thread, ble-wifi, onnetwork, code)
		// and gather necessary parameters (dataset, SSID/password).
		// The `payload.NodeIDToAssign` from frontend is a *suggestion*. Matter assigns the actual Node ID.
		// We pass it to chip-tool, which will use it if available, otherwise, a new one is picked.
		// We need to parse the *actual* Node ID from chip-tool's output.

		// Example using `pairing code` for simplicity (often requires device to be on IP or specific BLE setup)
		// chip-tool pairing code <node_id_to_assign_if_known_else_new> <setup_code>
		// If using BLE for initial commissioning with a known discriminator:
		// chip-tool pairing ble-discriminator <discriminator> <setup_code> <node_id_to_assign>
		// Let's use ble-discriminator as it's more common for uncommissioned BLE devices.
		// The Node ID to assign is often a large random number for chip-tool.
		// The frontend sends a temp one, we can use that or generate one.
		// The actual node ID will be in the output.

		// Using a fixed temporary node ID for the command, as chip-tool might assign its own.
		// We need to parse the actual assigned Node ID from the output.
		tempCommissioningNodeID := "112233" // A placeholder, chip-tool will manage actual assignment.

		cmdArgs := []string{"pairing", "ble-discriminator", payload.Discriminator, payload.SetupCode, tempCommissioningNodeID}
		// if paaTrustStorePath != "" {
		// cmdArgs = append(cmdArgs, "--paa-trust-store-path", paaTrustStorePath)
		// }
		cmd := exec.Command(chipToolPath, cmdArgs...)

		client.notifyClientLog("commissioning_log", fmt.Sprintf("Executing: %s %s", chipToolPath, strings.Join(cmdArgs, " ")))

		var outBuf, errBuf strings.Builder
		cmd.Stdout = &outBuf
		cmd.Stderr = &errBuf
		
		err := cmd.Run()
		stdout := outBuf.String()
		stderr := errBuf.String()
		commissioningOutput := fmt.Sprintf("Stdout:\n%s\nStderr:\n%s", stdout, stderr)

		log.Printf("chip-tool pairing output:\n%s", commissioningOutput)
		client.notifyClientLog("commissioning_log", "Commissioning command output:\n"+commissioningOutput)

		if err != nil {
			errMsg := fmt.Sprintf("Error commissioning device: %v. Output: %s", err, commissioningOutput)
			log.Println(errMsg)
			client.sendPayload("commissioning_status", CommissioningStatusPayload{
				Success:               false,
				Error:                 errMsg,
				Details:               commissioningOutput,
				OriginalDiscriminator: payload.Discriminator,
				DiscriminatorAssociatedWithRequest: payload.Discriminator, // from original request
			})
			return
		}
		
		// Parse commissioning output for success and actual Node ID
		// This is highly dependent on chip-tool's output format.
		// Example success: "Successfully commissioned device with node ID 0x<ACTUAL_NODE_ID>"
		// Example success: "Device commissioning completed with success" followed by node ID info.
		reNodeID := regexp.MustCompile(`Successfully commissioned device with node ID (0x[0-9a-fA-F]+|\d+)`)
		matches := reNodeID.FindStringSubmatch(stdout)
		
		actualNodeID := ""
		if len(matches) > 1 {
			actualNodeID = matches[1]
			log.Printf("Successfully parsed commissioned Node ID: %s", actualNodeID)
			client.sendPayload("commissioning_status", CommissioningStatusPayload{
				Success:               true,
				NodeID:                actualNodeID,
				Details:               "Device commissioned successfully. " + commissioningOutput,
				OriginalDiscriminator: payload.Discriminator,
				DiscriminatorAssociatedWithRequest: payload.Discriminator,
			})
			// Optional: Automatically start subscription or read initial attributes
			go readAttribute(client, actualNodeID, "1", "BasicInformation", "NodeLabel") // Example read
		} else if strings.Contains(stdout, "Device commissioning completed with success") || strings.Contains(stdout, "Commissioning success") {
			// Could not parse NodeID directly, but success message found.
			// This might happen if node ID is logged differently or if we need to query it.
			log.Printf("Commissioning reported success for discriminator %s, but Node ID not parsed directly from output.", payload.Discriminator)
			client.sendPayload("commissioning_status", CommissioningStatusPayload{
				Success:               true,
				Details:               "Commissioning reported success, but Node ID parsing needs verification. Output: " + commissioningOutput,
				OriginalDiscriminator: payload.Discriminator,
				DiscriminatorAssociatedWithRequest: payload.Discriminator,
			})
		} else {
			log.Printf("Commissioning for discriminator %s may have failed or Node ID not found. Output: %s", payload.Discriminator, commissioningOutput)
			client.sendPayload("commissioning_status", CommissioningStatusPayload{
				Success:               false,
				Error:                 "Commissioning finished, but success or Node ID unclear. Check logs.",
				Details:               commissioningOutput,
				OriginalDiscriminator: payload.Discriminator,
				DiscriminatorAssociatedWithRequest: payload.Discriminator,
			})
		}

	case "device_command":
		var payload DeviceCommandPayload
		payloadBytes, _ := json.Marshal(msg.Payload)
		if err := json.Unmarshal(payloadBytes, &payload); err != nil {
			client.notifyClientLog("command_response", "Invalid payload for device_command: "+err.Error()) // Use specific log type if frontend expects it
			client.sendPayload("command_response", CommandResponsePayload{Success: false, Error: "Invalid payload: " + err.Error()})
			return
		}

		log.Printf("Handling device_command request: %+v", payload)
		if payload.NodeID == "" || payload.Cluster == "" || payload.Command == "" {
			client.sendPayload("command_response", CommandResponsePayload{Success: false, NodeID: payload.NodeID, Error: "Missing nodeId, cluster, or command"})
			return
		}

		// Endpoint ID is often 1 for the primary function. This is a simplification.
		endpointID := "1" // Default endpoint

		// chip-tool <cluster_lowercase> <command_lowercase> <param1> <param2> ... <nodeId> <endpointId>
		// Note: chip-tool argument order can vary. For commands like onoff 'on', it's:
		// chip-tool onoff on <nodeId> <endpointId>
		// For levelcontrol 'move-to-level', it's:
		// chip-tool levelcontrol move-to-level <level> <transitionTime> <optionMask> <optionOverride> <nodeId> <endpointId>

		cmdArgs := []string{strings.ToLower(payload.Cluster), strings.ToLower(payload.Command)}

		// Add specific parameters for commands
		switch payload.Cluster {
		case "OnOff":
			// No additional params for On, Off, Toggle before nodeId and endpointId
		case "LevelControl":
			if payload.Command == "MoveToLevel" {
				levelVal, okL := payload.Params["level"].(float64) // JSON numbers are float64
				// transitionTime, optionMask, optionOverride often 0 for simple cases
				ttVal, okTT := payload.Params["transitionTime"].(float64)
				// omVal, okOM := payload.Params["optionMask"].(float64)
				// ooVal, okOO := payload.Params["optionOverride"].(float64)

				if !okL {
					client.sendPayload("command_response", CommandResponsePayload{Success: false, NodeID: payload.NodeID, Error: "Missing or invalid 'level' parameter for MoveToLevel"})
					return
				}
				cmdArgs = append(cmdArgs, strconv.Itoa(int(levelVal)))
				if okTT { // transitionTime is optional for chip-tool if 0
					cmdArgs = append(cmdArgs, strconv.Itoa(int(ttVal)))
				} else {
					cmdArgs = append(cmdArgs, "0") // Default transition time
				}
				// Default optionMask and optionOverride
				cmdArgs = append(cmdArgs, "0", "0") 
			}
		// Add more cluster/command specific parameter handling here
		default:
			// For generic commands, try to append params if any, assuming they are simple string values
			for k, v := range payload.Params {
				client.notifyClientLog("command_response", fmt.Sprintf("Warning: Generic param handling for %s.%s - %s:%v. May not be correct for chip-tool.", payload.Cluster, payload.Command, k, v))
				cmdArgs = append(cmdArgs, fmt.Sprintf("%v", v))
			}
		}

		cmdArgs = append(cmdArgs, payload.NodeID, endpointID)
		cmd := exec.Command(chipToolPath, cmdArgs...)
		client.notifyClientLog("command_response", fmt.Sprintf("Executing: %s %s", chipToolPath, strings.Join(cmdArgs, " ")))


		var outBuf, errBuf strings.Builder
		cmd.Stdout = &outBuf
		cmd.Stderr = &errBuf

		err := cmd.Run()
		stdout := outBuf.String()
		stderr := errBuf.String()
		cmdOutput := fmt.Sprintf("Stdout:\n%s\nStderr:\n%s", stdout, stderr)

		log.Printf("chip-tool command output for %s.%s on %s:\n%s", payload.Cluster, payload.Command, payload.NodeID, cmdOutput)
		
		if err != nil {
			errMsg := fmt.Sprintf("Error executing %s.%s on %s: %v. Output: %s", payload.Command, payload.Cluster, payload.NodeID, err, cmdOutput)
			log.Println(errMsg)
			client.sendPayload("command_response", CommandResponsePayload{Success: false, NodeID: payload.NodeID, Error: errMsg, Details: cmdOutput})
			return
		}
		
		// Simplistic success check. chip-tool might not always give clear success/failure in exit code for all commands.
		// Look for "CHIP Error" or "Error:" in output.
		if strings.Contains(stdout, "CHIP Error") || strings.Contains(stderr, "CHIP Error") || strings.Contains(stderr, "Error:") {
			errMsg := "Command executed but chip-tool reported an error in its output."
			log.Println(errMsg, "Details:", cmdOutput)
			client.sendPayload("command_response", CommandResponsePayload{Success: false, NodeID: payload.NodeID, Error: errMsg, Details: cmdOutput})
		} else {
			log.Printf("Command %s.%s on Node %s executed. Output: %s", payload.Cluster, payload.Command, payload.NodeID, cmdOutput)
			client.sendPayload("command_response", CommandResponsePayload{Success: true, NodeID: payload.NodeID, Details: "Command executed. Output: " + cmdOutput})

			// After a command, try to read the relevant attribute to update UI
			// This makes the UI more responsive to state changes.
			// A more robust solution would use subscriptions.
			if payload.Cluster == "OnOff" && (payload.Command == "On" || payload.Command == "Off" || payload.Command == "Toggle") {
				go readAttribute(client, payload.NodeID, endpointID, "OnOff", "OnOff")
			}
			if payload.Cluster == "LevelControl" && payload.Command == "MoveToLevel" {
				go readAttribute(client, payload.NodeID, endpointID, "LevelControl", "CurrentLevel")
			}
		}
	
	// **** ADD THIS NEW CASE for handling subscriptions ****
	case "subscribe_attribute":
		var payload SubscribeAttributePayload
		payloadBytes, _ := json.Marshal(msg.Payload)
		if err := json.Unmarshal(payloadBytes, &payload); err != nil {
			client.notifyClientLog("subscription_log", "Invalid payload for subscribe_attribute: "+err.Error())
			// Consider sending a specific error response type if frontend needs it
			client.notifyClient("error", map[string]interface{}{"message": "Invalid subscribe_attribute payload: " + err.Error()})
			return
		}
		log.Printf("Handling subscribe_attribute request: %+v", payload)

		if payload.NodeID == "" || payload.Cluster == "" || payload.Attribute == "" || payload.MinInterval == "" || payload.MaxInterval == "" {
			client.notifyClientLog("subscription_log", "Missing parameters for subscribe_attribute.")
			client.notifyClient("error", map[string]interface{}{"message": "Missing parameters for subscribe_attribute (nodeId, cluster, attribute, minInterval, maxInterval required)."})
			return
		}
		epId := payload.EndpointID
		if epId == "" {
			epId = "1" // Default to endpoint 1 if not specified
		}
		// Launch the subscription in a new goroutine.
		// Note: This doesn't store/manage the *exec.Cmd process for later termination.
		// For a real app, you'd want to store cmd instances to kill them on client disconnect or unsubscribe request.
		go startAttributeSubscription(client, payload.NodeID, epId, payload.Cluster, payload.Attribute, payload.MinInterval, payload.MaxInterval)


	default:
		log.Printf("Unknown message type from client %v: %s", client.conn.RemoteAddr(), msg.Type)
		client.notifyClient("error", map[string]interface{}{"message": "Unknown command type received: " + msg.Type})
	}
}

// notifyClientLog sends a log message to a specific client.
// The frontend's wizardStore.handleBackendMessage expects specific types for logs.
func (c *Client) notifyClientLog(logType string, data string) {
	// logType could be "discovery_log", "commissioning_log", "hub_setup_log" etc.
	// The frontend's TypedServerToClientMessage expects payload for these.
	msg := ServerMessage{Type: logType, Payload: data}
	bytes, err := json.Marshal(msg)
	if err != nil {
		log.Printf("Error marshalling log message for client %v: %v", c.conn.RemoteAddr(), err)
		return
	}
	select {
	case c.send <- bytes:
	default:
		log.Printf("Client %v send channel full, log message dropped: %s", c.conn.RemoteAddr(), logType)
	}
}

// notifyClient sends a generic message to a specific client.
// Used for simpler messages or errors not fitting specific payload structures.
func (c *Client) notifyClient(msgType string, payload interface{}) {
	msg := ServerMessage{Type: msgType, Payload: payload}
	bytes, err := json.Marshal(msg)
	if err != nil {
		log.Printf("Error marshalling server message for client %v: %v", c.conn.RemoteAddr(), err)
		return
	}
	select {
	case c.send <- bytes:
	default:
		log.Printf("Client %v send channel full, message dropped: %s", c.conn.RemoteAddr(), msgType)
	}
}

// sendPayload is a helper to send strongly-typed payloads.
func (c *Client) sendPayload(msgType string, payload interface{}) {
	// This is essentially the same as notifyClient, but emphasizes structured payloads.
	c.notifyClient(msgType, payload)
}

// parseDiscoveryOutput parses the output of `chip-tool discover commissionable`
func parseDiscoveryOutput(output string, client *Client) []DiscoveredDevice {
	var devices []DiscoveredDevice
	var currentDevice *DiscoveredDevice // Use a pointer to modify the current device being built

	scanner := bufio.NewScanner(strings.NewReader(output))

	// Regex to clean up the prefix of each relevant line, e.g., "[timestamp][pid:tid][DIS]"
	linePrefixRegex := regexp.MustCompile(`^\[.*?\]\s\[.*?\]\s\[DIS\]\s+`)

	// Regexes for parsing specific key-value pairs from the cleaned line
	// Making these more specific to the keys found in your output.
	hostnameRegex := regexp.MustCompile(`^Hostname:\s*(.*)`)
	ipAddressRegex := regexp.MustCompile(`^IP Address #\d+:\s*(.*)`) // Captures any IP address line
	portRegex := regexp.MustCompile(`^Port:\s*(\d+)`)
	vendorIDRegex := regexp.MustCompile(`^Vendor ID:\s*(\d+)`)
	productIDRegex := regexp.MustCompile(`^Product ID:\s*(\d+)`)
	longDiscriminatorRegex := regexp.MustCompile(`^Long Discriminator:\s*(\d+)`) // Using Long Discriminator
	pairingHintRegex := regexp.MustCompile(`^Pairing Hint:\s*(\d+)`)
	instanceNameRegex := regexp.MustCompile(`^Instance Name:\s*([0-9A-Fa-f]+)`)
	commissioningModeRegex := regexp.MustCompile(`^Commissioning Mode:\s*(\d+)`)
	// Device Name might also be present in some outputs, add if needed:
	// deviceNameRegex := regexp.MustCompile(`^Device Name:\s*(.*)`)


	for scanner.Scan() {
		line := scanner.Text()
		// Send each raw line to client for debugging if needed by uncommenting in handlers.go
		// client.notifyClientLog("discovery_log", "Raw line: "+line)

		cleanLine := linePrefixRegex.ReplaceAllString(line, "")
		trimmedCleanLine := strings.TrimSpace(cleanLine)

		if strings.HasPrefix(trimmedCleanLine, "Discovered commissionable/commissioner node:") {
			// Start of a new device block.
			// If there was a previous device being built and it's valid, add it to the list.
			if currentDevice != nil && (currentDevice.Discriminator != "" || currentDevice.InstanceName != "") { // Check for essential fields
				// Ensure a unique ID is set
				if currentDevice.ID == "" {
					if currentDevice.InstanceName != "" {
						currentDevice.ID = fmt.Sprintf("dnsd_instance_%s", currentDevice.InstanceName)
					} else {
						currentDevice.ID = fmt.Sprintf("dnsd_vid%s_pid%s_disc%s", currentDevice.VendorID, currentDevice.ProductID, currentDevice.Discriminator)
					}
				}
				// Ensure a name is set if not parsed
				if currentDevice.Name == "" {
					currentDevice.Name = fmt.Sprintf("MatterDevice-%s", currentDevice.InstanceName)
				}
				devices = append(devices, *currentDevice)
				client.notifyClientLog("discovery_log", fmt.Sprintf("Completed parsing device: %+v", *currentDevice))
			}
			// Initialize a new device
			currentDevice = &DiscoveredDevice{}
			client.notifyClientLog("discovery_log", "New device block started by: "+trimmedCleanLine)
			continue
		}

		// If we are inside a device block (currentDevice is not nil)
		if currentDevice != nil {
			if m := hostnameRegex.FindStringSubmatch(trimmedCleanLine); len(m) > 1 {
				currentDevice.Name = m[1] // Using Hostname as a potential Name
				client.notifyClientLog("discovery_log", fmt.Sprintf("Parsed Hostname: %s", m[1]))
			} else if m := ipAddressRegex.FindStringSubmatch(trimmedCleanLine); len(m) > 1 {
				// We might have multiple IP addresses. For simplicity, store the first one.
				// The DiscoveredDevice struct doesn't have an IP field yet, but you could add it.
				// For now, just log it.
				client.notifyClientLog("discovery_log", fmt.Sprintf("Parsed IP Address: %s", m[1]))
			} else if m := portRegex.FindStringSubmatch(trimmedCleanLine); len(m) > 1 {
				// Port also not in current DiscoveredDevice struct, log for now.
				client.notifyClientLog("discovery_log", fmt.Sprintf("Parsed Port: %s", m[1]))
			} else if m := vendorIDRegex.FindStringSubmatch(trimmedCleanLine); len(m) > 1 {
				currentDevice.VendorID = m[1]
				client.notifyClientLog("discovery_log", fmt.Sprintf("Parsed Vendor ID: %s", m[1]))
			} else if m := productIDRegex.FindStringSubmatch(trimmedCleanLine); len(m) > 1 {
				currentDevice.ProductID = m[1]
				client.notifyClientLog("discovery_log", fmt.Sprintf("Parsed Product ID: %s", m[1]))
			} else if m := longDiscriminatorRegex.FindStringSubmatch(trimmedCleanLine); len(m) > 1 {
				currentDevice.Discriminator = m[1] // Using Long Discriminator
				client.notifyClientLog("discovery_log", fmt.Sprintf("Parsed Long Discriminator: %s", m[1]))
			} else if m := pairingHintRegex.FindStringSubmatch(trimmedCleanLine); len(m) > 1 {
				ph, _ := strconv.ParseUint(m[1], 10, 16)
				currentDevice.PairingHint = uint16(ph)
				client.notifyClientLog("discovery_log", fmt.Sprintf("Parsed Pairing Hint: %s", m[1]))
			} else if m := instanceNameRegex.FindStringSubmatch(trimmedCleanLine); len(m) > 1 {
				currentDevice.InstanceName = m[1] // Store instance name
				client.notifyClientLog("discovery_log", fmt.Sprintf("Parsed Instance Name: %s", m[1]))
			} else if m := commissioningModeRegex.FindStringSubmatch(trimmedCleanLine); len(m) > 1 {
				cm, _ := strconv.ParseUint(m[1], 10, 8)
				currentDevice.CommissioningMode = uint8(cm)
				// You might want to map this to a string like "BLE" or "OnNetwork" for the frontend
				// CM: 1 often means BLE, CM: 2 often means OnNetwork (DNS-SD)
				currentDevice.Type = fmt.Sprintf("CM:%d", cm) // Store raw CM as type for now
				client.notifyClientLog("discovery_log", fmt.Sprintf("Parsed Commissioning Mode: %s", m[1]))
			}
			// Add other regex checks here if needed for fields like "Device Name", "Device Type" etc.
			// Example:
			// else if m := deviceNameRegex.FindStringSubmatch(trimmedCleanLine); len(m) > 1 {
			// 	currentDevice.Name = m[1]
			// 	client.notifyClientLog("discovery_log", fmt.Sprintf("Parsed Device Name: %s", m[1]))
			// }
		}
	}

	// Add the last processed device if it's valid
	if currentDevice != nil && (currentDevice.Discriminator != "" || currentDevice.InstanceName != "") {
		if currentDevice.ID == "" {
			if currentDevice.InstanceName != "" {
				currentDevice.ID = fmt.Sprintf("dnsd_instance_%s", currentDevice.InstanceName)
			} else {
				currentDevice.ID = fmt.Sprintf("dnsd_vid%s_pid%s_disc%s", currentDevice.VendorID, currentDevice.ProductID, currentDevice.Discriminator)
			}
		}
		if currentDevice.Name == "" && currentDevice.InstanceName != "" { // Prefer InstanceName if Hostname wasn't parsed as Name
		    currentDevice.Name = fmt.Sprintf("MatterDevice-%s", currentDevice.InstanceName)
		} else if currentDevice.Name == "" { // Fallback if no Hostname or InstanceName
			currentDevice.Name = fmt.Sprintf("MatterDevice-VID%s-PID%s", currentDevice.VendorID, currentDevice.ProductID)
		}
		devices = append(devices, *currentDevice)
		client.notifyClientLog("discovery_log", fmt.Sprintf("Completed parsing final device: %+v", *currentDevice))
	}

	if len(devices) == 0 {
		client.notifyClientLog("discovery_log", "No devices parsed from output. Check chip-tool output and parsing logic.")
	} else {
		client.notifyClientLog("discovery_log", fmt.Sprintf("Successfully parsed %d device(s).", len(devices)))
	}

	return devices
}


// readAttribute function to read a specific attribute from a device
func readAttribute(client *Client, nodeID, endpointID, clusterName, attributeName string) {
	log.Printf("Attempting to read attribute %s.%s for Node %s Endpoint %s", clusterName, attributeName, nodeID, endpointID)
	client.notifyClientLog("commissioning_log", fmt.Sprintf("Reading attribute %s.%s for Node %s...", clusterName, attributeName, nodeID)) // Use a generic log type or command_response

	// chip-tool <cluster_lowercase> read <attribute_lowercase> <nodeId> <endpointId>
	cmdArgs := []string{
		strings.ToLower(clusterName),
		"read",
		strings.ToLower(attributeName), // chip-tool usually expects PascalCase for attribute names in read commands
		nodeID,
		endpointID,
	}
	cmd := exec.Command(chipToolPath, cmdArgs...)
	
	var outBuf, errBuf strings.Builder
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf

	err := cmd.Run()
	stdout := outBuf.String()
	stderr := errBuf.String()
	cmdOutput := fmt.Sprintf("Read Attribute Stdout:\n%s\nRead Attribute Stderr:\n%s", stdout, stderr)
	log.Println(cmdOutput)


	if err != nil {
		log.Printf("Error reading attribute %s.%s for Node %s: %v. Output: %s", clusterName, attributeName, nodeID, err, cmdOutput)
		client.notifyClientLog("commissioning_log", fmt.Sprintf("Failed to read attribute %s.%s: %v", clusterName, attributeName, err))
		return
	}

	// Parse the output to find the value. This is highly dependent on chip-tool's output format.
	// Example output for OnOff: "CHIP:DMG: Value = true" or "CHIP:DMG: Value = false"
	// Example output for LevelControl CurrentLevel: "CHIP:DMG: Value = 128"
	// Example output for BasicInformation NodeLabel: "CHIP:DMG: Value = "My Light"" (string value)
	
	var value interface{}
	parsed := false

	// Try to parse common patterns
	reValue := regexp.MustCompile(`CHIP:DMG: Value = (.*)`) // General value capture
	matches := reValue.FindStringSubmatch(stdout)

	if len(matches) > 1 {
		valStr := strings.TrimSpace(matches[1])
		// Try to convert to boolean
		if bVal, err := strconv.ParseBool(valStr); err == nil {
			value = bVal
			parsed = true
		} else if iVal, err := strconv.ParseInt(valStr, 10, 64); err == nil { // Try to convert to int
			value = iVal
			parsed = true
		} else if fVal, err := strconv.ParseFloat(valStr, 64); err == nil { // Try to convert to float
			value = fVal
			parsed = true
		} else {
			// Assume string if it's quoted, or just take as is
			if strings.HasPrefix(valStr, `"`) && strings.HasSuffix(valStr, `"`) {
				value = strings.Trim(valStr, `"`)
			} else {
				value = valStr // Raw string
			}
			parsed = true
		}
	}

	if !parsed {
		log.Printf("Could not parse value for attribute %s.%s from output: %s", clusterName, attributeName, stdout)
		client.notifyClientLog("commissioning_log", fmt.Sprintf("Could not parse value for %s.%s", clusterName, attributeName))
		// Send raw output if parsing fails but command succeeded
		value = "Raw: " + stdout 
	}
	
	log.Printf("Attribute %s.%s for Node %s read. Value: %v (Parsed: %t)", clusterName, attributeName, nodeID, value, parsed)

	client.sendPayload("attribute_update", AttributeUpdatePayload{
		NodeID:    nodeID,
		EndpointID: endpointID,
		Cluster:   clusterName,
		Attribute: attributeName,
		Value:     value,
	})
}


// **** NEW FUNCTION: startAttributeSubscription ****
func startAttributeSubscription(client *Client, nodeID, endpointID, clusterName, attributeName, minInterval, maxInterval string) {
	subscriptionID := fmt.Sprintf("sub-%s-%s-%s-%s", nodeID, endpointID, clusterName, attributeName)
	log.Printf("[%s] Starting subscription for Node %s, Endpoint %s, Cluster %s, Attribute %s, MinInterval %ss, MaxInterval %ss",
		subscriptionID, nodeID, endpointID, clusterName, attributeName, minInterval, maxInterval)

	client.notifyClientLog("subscription_log", fmt.Sprintf("Attempting to subscribe to %s/%s on Node %s EP%s", clusterName, attributeName, nodeID, endpointID))

	// chip-tool <cluster_lowercase> subscribe <attribute_pascalcase> <min_interval> <max_interval> <node_id> <endpoint_id>
	// Note: chip-tool expects PascalCase for attribute names in subscribe commands.
	cmdArgs := []string{
		strings.ToLower(clusterName),
		"subscribe",
		attributeName, // PascalCase
		minInterval,
		maxInterval,
		nodeID,
		endpointID,
	}
	
	// For robust management, use context to allow canceling the command.
	// ctx, cancel := context.WithCancel(context.Background())
	// cmd := exec.CommandContext(ctx, chipToolPath, cmdArgs...)
	cmd := exec.Command(chipToolPath, cmdArgs...)

	// TODO: Store the 'cmd' and 'cancel' function in the Client struct (e.g., in a map keyed by subscriptionID)
	// to allow stopping this subscription later (e.g., on client disconnect or explicit unsubscribe message).
	// client.subMu.Lock()
	// client.activeSubscriptions[subscriptionID] = cmd
	// client.subMu.Unlock()
	// defer func() {
	// 	 client.subMu.Lock()
	// 	 delete(client.activeSubscriptions, subscriptionID)
	// 	 client.subMu.Unlock()
	// 	 cancel() // Ensure context is cancelled if command finishes or goroutine exits
	// }()


	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		log.Printf("[%s] Error creating stdout pipe for subscription: %v", subscriptionID, err)
		client.notifyClientLog("subscription_log", fmt.Sprintf("Error starting subscription pipe for %s: %v", attributeName, err))
		return
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		log.Printf("[%s] Error creating stderr pipe for subscription: %v", subscriptionID, err)
		client.notifyClientLog("subscription_log", fmt.Sprintf("Error starting subscription stderr pipe for %s: %v", attributeName, err))
		return
	}

	if err := cmd.Start(); err != nil {
		log.Printf("[%s] Error starting chip-tool subscribe command: %v", subscriptionID, err)
		client.notifyClientLog("subscription_log", fmt.Sprintf("Error starting subscription command for %s: %v", attributeName, err))
		return
	}

	log.Printf("[%s] chip-tool subscribe process started (PID: %d). Monitoring output.", subscriptionID, cmd.Process.Pid)
	client.notifyClientLog("subscription_log", fmt.Sprintf("Subscription process started for %s/%s.", clusterName, attributeName))

	// Goroutine to read stderr
	go func() {
		scanner := bufio.NewScanner(stderrPipe)
		for scanner.Scan() {
			line := scanner.Text()
			log.Printf("[%s] Stderr: %s", subscriptionID, line)
			client.notifyClientLog("subscription_log", fmt.Sprintf("[%s] Error Stream: %s", attributeName, line))
		}
		if err := scanner.Err(); err != nil {
			log.Printf("[%s] Error reading stderr for subscription: %v", subscriptionID, err)
		}
		log.Printf("[%s] Stderr pipe closed.", subscriptionID)
	}()

	// Goroutine to read stdout and parse reports
	go func() {
		scanner := bufio.NewScanner(stdoutPipe)
		// Regex to parse the data part of a report
		// Example: CHIP:DMG:    Data = true (BOOLEAN)
		// Example: CHIP:DMG:    Data = 123 (INT16S)
		// Example: CHIP:DMG:    Data = "Hello" (UTF8S) - Note: chip-tool might not always quote strings in this output.
		reDataLine := regexp.MustCompile(`CHIP:DMG:\s+Data = (.*) \((.*)\)`)
		// Regex to find the start of a report block
		reReportStart := regexp.MustCompile(`CHIP:DMG: ReportDataMessage =`)
		// Regex to find the attribute path (optional, for verification)
		// reAttributePath := regexp.MustCompile(`AttributePath = Cluster: (\d+) Endpoint: (\d+) Attribute: (\d+)`)

		inReportBlock := false

		for scanner.Scan() {
			line := scanner.Text()
			log.Printf("[%s] Stdout: %s", subscriptionID, line) // Log all stdout for debugging

			if reReportStart.MatchString(line) {
				inReportBlock = true
				log.Printf("[%s] Detected report start.", subscriptionID)
				continue
			}

			if inReportBlock {
				if matches := reDataLine.FindStringSubmatch(line); len(matches) == 3 {
					valStr := strings.TrimSpace(matches[1])
					typeStr := strings.TrimSpace(matches[2])
					log.Printf("[%s] Parsed data line: ValueStr='%s', TypeStr='%s'", subscriptionID, valStr, typeStr)

					var value interface{}
					var parseErr error

					switch typeStr {
					case "BOOLEAN":
						value, parseErr = strconv.ParseBool(valStr)
					case "INT8S", "INT16S", "INT32S", "INT64S", "UINT8", "UINT16", "UINT32", "UINT64", "INT8U", "INT16U", "INT32U", "INT64U": // Common integer types
						value, parseErr = strconv.ParseInt(valStr, 10, 64)
					case "FLOAT", "DOUBLE": // Common float types
						value, parseErr = strconv.ParseFloat(valStr, 64)
					case "UTF8S", "OCTET_STRING": // String types
						// chip-tool might or might not quote strings. Trim quotes if present.
						if strings.HasPrefix(valStr, `"`) && strings.HasSuffix(valStr, `"`) {
							value = strings.Trim(valStr, `"`)
						} else {
							value = valStr
						}
					default:
						log.Printf("[%s] Unhandled data type from subscription: %s. Storing as string.", subscriptionID, typeStr)
						value = valStr // Store as raw string if type is unknown
					}

					if parseErr != nil {
						log.Printf("[%s] Error parsing value '%s' as type '%s': %v. Storing as raw string.", subscriptionID, valStr, typeStr, parseErr)
						value = valStr // Fallback to raw string on parsing error
					}
					
					log.Printf("[%s] Sending attribute_update: Node=%s, Cls=%s, Attr=%s, Val=%v", subscriptionID, nodeID, clusterName, attributeName, value)
					client.sendPayload("attribute_update", AttributeUpdatePayload{
						NodeID:    nodeID,
						EndpointID: endpointID,
						Cluster:   clusterName, 
						Attribute: attributeName, 
						Value:     value,
					})
					inReportBlock = false // Reset for next report, simple assumption one data line per report block
				} else if strings.Contains(line, "CHIP:DMG: }") { // End of a report block
                    inReportBlock = false
					log.Printf("[%s] Detected report end.", subscriptionID)
                }
			}
		}
		if err := scanner.Err(); err != nil {
			log.Printf("[%s] Error reading stdout for subscription: %v", subscriptionID, err)
			client.notifyClientLog("subscription_log", fmt.Sprintf("[%s] Error reading subscription stream: %v", attributeName, err))
		}
		log.Printf("[%s] Stdout pipe closed.", subscriptionID)

		// Wait for the command to finish
		err = cmd.Wait()
		log.Printf("[%s] chip-tool subscribe command finished. Exit error: %v", subscriptionID, err)
		client.notifyClientLog("subscription_log", fmt.Sprintf("Subscription for %s/%s on Node %s ended. Error: %v", clusterName, attributeName, nodeID, err))
		// TODO: Clean up from client.activeSubscriptions if it was stored
	}()
}