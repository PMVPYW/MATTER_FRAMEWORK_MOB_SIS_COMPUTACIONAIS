package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"os"
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
	chipToolPath = "/snap/bin/chip-tool" // IMPORTANT: Verify this path on your RPi
	paaTrustStorePath = "/paa-root-certs/dcld_mirror_CN_Basics_PAA_vid_0x137B.der"

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
	// activeSubscriptions map[string]*exec.Cmd // For robust subscription management
	// subMu sync.Mutex
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
		// TODO: When a client disconnects, all its active subscriptions should be stopped.
		// This would involve iterating c.activeSubscriptions and calling cmd.Process.Kill()
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

		var clientMsg ClientMessage // Assuming ClientMessage is defined in models.go
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
		c.conn.Close()
		log.Printf("Client %v disconnected from writePump", c.conn.RemoteAddr())
	}()
	for {
		select {
		case message, ok := <-c.send:
			c.writeMu.Lock() // Protect concurrent writes
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				// The hub closed the channel.
				log.Printf("Client %v send channel closed, sending close message.", c.conn.RemoteAddr())
				_ = c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				c.writeMu.Unlock()
				return
			}

			// Send the message as a whole. No batching with NextWriter.
			err := c.conn.WriteMessage(websocket.TextMessage, message)
			if err != nil {
				log.Printf("Client %v error writing message: %v", c.conn.RemoteAddr(), err)
				c.writeMu.Unlock()
				return // Exit on write error
			}
			c.writeMu.Unlock()

		case <-ticker.C:
			c.writeMu.Lock() // Protect concurrent writes
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
	// For robust subscription management, initialize activeSubscriptions map here:
	// client := &Client{hub: hub, conn: conn, send: make(chan []byte, 256), activeSubscriptions: make(map[string]*exec.Cmd)}
	client := &Client{hub: hub, conn: conn, send: make(chan []byte, 256)}
	client.hub.register <- client

	log.Printf("Client %v connected via WebSocket", conn.RemoteAddr())

	go client.writePump()
	go client.readPump()
}

// ANSI escape code stripper
var ansiRegex = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)

func stripAnsi(str string) string {
	return ansiRegex.ReplaceAllString(str, "")
}

// handleClientMessage processes messages from the client and interacts with chip-tool.
func handleClientMessage(client *Client, msg ClientMessage) { // ClientMessage should be defined in models.go
	switch msg.Type {
	case "discover_devices":
		log.Println("Handling discover_devices request (for 'commissionables' devices)")
		client.notifyClientLog("discovery_log", "Starting 'discover commissionables' via chip-tool...")

		discoveryTimeout := 60 * time.Second // Adjust as needed

		ctx, cancel := context.WithTimeout(context.Background(), discoveryTimeout)
		defer cancel() // Ensure context resources are cleaned up

		// cmd := exec.CommandContext(ctx, chipToolPath, "discover", "commissionables", "--discover-once", "false")
		cmd := exec.CommandContext(ctx, chipToolPath, "discover", "commissionables")
		var outBuf, errBuf strings.Builder
		cmd.Stdout = &outBuf
		cmd.Stderr = &errBuf

		err := cmd.Run() // This will block until the command completes, errors, or the context times out.

		stdout := outBuf.String()
		stderr := errBuf.String()

		if stdout != "" {
			log.Printf("chip-tool 'discover commissionables' stdout:\n%s", stdout)
		} else {
			log.Println("chip-tool 'discover commissionables' stdout was empty.")
		}
		if stderr != "" {
			log.Printf("chip-tool 'discover commissionables' stderr:\n%s", stderr)
		}


			errMsg := ""
			if ctx.Err() == context.DeadlineExceeded {
				errMsg = fmt.Sprintf("Discovery command timed out after %s. Stdout: %s, Stderr: %s", discoveryTimeout, stdout, stderr)
				log.Println(errMsg)
				client.notifyClientLog("discovery_log", "Discovery timed out: "+errMsg)
			} else {
				errMsg = fmt.Sprintf("Error running chip-tool 'discover commissionables': %v. Stdout: %s, Stderr: %s", err, stdout, stderr)
				log.Println(errMsg)
				client.notifyClientLog("discovery_log", "Error during discovery: "+errMsg)
			}
			fmt.Println("ConaAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAa", DiscoveryResultPayload{Devices: []DiscoveredDevice{}, Error: errMsg})
			
			client.sendPayload("discovery_result", DiscoveryResultPayload{Devices: []DiscoveredDevice{}, Error: errMsg})


		// If err is nil, the command completed successfully (exit status 0) before the timeout.
		// This is unlikely for "discover --discover-once false" unless chip-tool has internal logic to stop.
		client.notifyClientLog("discovery_log", "Discovery command 'discover commissionables' finished. Output processing...")
		discovered := parseDiscoveryOutput(stdout, client)
		client.sendPayload("discovery_result", DiscoveryResultPayload{Devices: discovered})

	case "commission_device":
		var payload CommissionDevicePayload // Assumes CommissionDevicePayload is in models.go
		payloadBytes, _ := json.Marshal(msg.Payload)
		if err := json.Unmarshal(payloadBytes, &payload); err != nil {
			client.notifyClientLog("commissioning_log", "Invalid payload for commission_device: "+err.Error())
			client.sendPayload("commissioning_status", CommissioningStatusPayload{Success: false, Error: "Invalid payload: " + err.Error()}) // Assumes CommissioningStatusPayload is in models.go
			return
		}
		log.Printf("Handling commission_device request: %+v", payload)
		if payload.SetupCode == "" { // Discriminator might not be strictly needed for 'pairing code' if device is uniquely identified by IP context
			client.notifyClientLog("commissioning_log", "Missing setupCode or nodeIdToAssign for commissioning.")
			client.sendPayload("commissioning_status", CommissioningStatusPayload{Success: false, Error: "Missing setupCode or nodeIdToAssign.", OriginalDiscriminator: payload.LongDiscriminator})
			return
		}
		
		client.notifyClientLog("commissioning_log", fmt.Sprintf("Attempting to commission Node ID %s with setup code %s (using 'pairing code')", payload.CommissioningMode, payload.SetupCode))

		// **** UPDATED Commissioning Command for IP-based devices ****
		// Using `pairing code` which is suitable for devices already on the IP network.
		// The payload.NodeIDToAssign is a suggestion from the frontend for the new node.
		// chip-tool will manage the actual assignment.

		var _, err = os.Getwd()
		if err != nil {
			fmt.Println("Error getting current working directory:", err)
			return
		}
		payload.NodeID = fmt.Sprintf("%04d", rand.Intn(100000))
				fmt.Println("\n FDS NODE ID:",  payload.NodeID)

		cmdArgs := []string{"pairing", "onnetwork-long", payload.NodeID, payload.SetupCode, payload.LongDiscriminator}
		fmt.Println("\nCMDARGS:",  cmdArgs)
		fmt.Println("\nPAYLOAD:",  payload)
		fmt.Println("\nPAYLOAD NODE ID TO ASSIGN:",  payload.CommissioningMode	)
		fmt.Println("\nPAYLOAD Discriminator:",  payload.LongDiscriminator)
		fmt.Println("\nPAYLOAD ProductID:",  payload.ProductID)
		fmt.Println("\nPAYLOAD SetupCode:",  payload.SetupCode)
		fmt.Println("\nPAYLOAD VendorID:",  payload.VendorID)
		// cmdArgs := []string{"pairing", "onnetwork-long", payload.NodeIDToAssign, payload.SetupCode, payload.Discriminator}
		
		// if paaTrustStorePath != "" { // Add PAA trust store if needed for production devices
		//    cmdArgs = append(cmdArgs, "--paa-trust-store-path", paaTrustStorePath)
		// }

		cmd := exec.Command(chipToolPath, cmdArgs...)
		fmt.Println("[DEBUG - TESTE - COMMISSIONABLES] - CMD", cmd)
		fmt.Println("[DEBUG - TESTE - COMMISSIONABLES] - CMD", strings.Join(cmdArgs, " "))
		client.notifyClientLog("commissioning_log", fmt.Sprintf("Executing: %s %s", chipToolPath, strings.Join(cmdArgs, " ")))
		var outBuf, errBuf strings.Builder 
		cmd.Stdout = &outBuf
		cmd.Stderr = &errBuf
		err = cmd.Run()            
		stdout := outBuf.String()   
		stderr := errBuf.String()   
		commissioningOutput := fmt.Sprintf("Stdout:\n%s\nStderr:\n%s", stdout, stderr)
		log.Printf("chip-tool pairing output:\n%s", commissioningOutput)
		client.notifyClientLog("commissioning_log", "Commissioning command output:\n"+commissioningOutput)


		cmdArgs = []string{"descriptor", "read", "parts-list", payload.NodeID, "0"}
		
		cmd = exec.Command(chipToolPath, cmdArgs...)

		cmd.Stdout = &outBuf
		cmd.Stderr = &errBuf
		err = cmd.Run()            
		stdout = outBuf.String()   
		stderr = errBuf.String()  

		re := regexp.MustCompile(`Data = \[\s*(?:\[\d+\.\d+\] \[\d+:\d+\] \[DMG\]\s*)*([0-9]+) \(unsigned\)`)

		match := re.FindStringSubmatch(stdout)

		
		if err != nil && len(match)<1 {
			errMsg := fmt.Sprintf("Error commissioning device: %v. Output: %s", err, commissioningOutput)
			log.Println(errMsg)
			client.sendPayload("commissioning_status", CommissioningStatusPayload{
				Success:               false,
				Error:                 errMsg,
				Details:               commissioningOutput,
				OriginalDiscriminator: payload.LongDiscriminator, // Still useful to send back for frontend context
				DiscriminatorAssociatedWithRequest: payload.LongDiscriminator,
			})
			return
		}
		
		// Parse commissioning output for success and actual Node ID
		// reNodeID := regexp.MustCompile(`Successfully commissioned device with node ID (0x[0-9a-fA-F]+|\d+)`)
		
		log.Printf("Successfully parsed commissioned Node ID: %s", payload.NodeID)
		fmt.Println("\nTest: ", len(match))
		if (len(match) < 2) {
			return;
		}
		client.sendPayload("commissioning_status", CommissioningStatusPayload{
			Success:               true,
			NodeID:                payload.NodeID,
			Details:               "Device commissioned successfully. " + commissioningOutput,
			EndpointId: 			match[1],
			OriginalDiscriminator: payload.LongDiscriminator,
			DiscriminatorAssociatedWithRequest: payload.LongDiscriminator,
		})

		go readAttribute(client, payload.NodeID, "1", "BasicInformation", "NodeLabel")
		 
		if strings.Contains(stdout, "Commissioning success") || strings.Contains(stdout, "commissioning complete") || 
		            strings.Contains(stderr, "Commissioning success") || strings.Contains(stderr, "commissioning complete") && stderr == "" { // Added check for empty stderr
			log.Printf("Commissioning reported success (discriminator %s), but Node ID not directly parsed. Output: %s", payload.LongDiscriminator, commissioningOutput)
			client.sendPayload("commissioning_status", CommissioningStatusPayload{
				Success:               true, // Assume success based on message
				Details:               "Commissioning reported success. Node ID may need to be queried or was already known. Output: " + commissioningOutput,
				OriginalDiscriminator: payload.LongDiscriminator,
				DiscriminatorAssociatedWithRequest: payload.LongDiscriminator,
			})
		} else {
			log.Printf("Commissioning for discriminator %s may have failed or Node ID not found. Output: %s", payload.LongDiscriminator, commissioningOutput)
			client.sendPayload("commissioning_status", CommissioningStatusPayload{
				Success:               false,
				Error:                 "Commissioning finished, but success or Node ID unclear from output. Check logs.",
				Details:               commissioningOutput,
				OriginalDiscriminator: payload.LongDiscriminator,
				DiscriminatorAssociatedWithRequest: payload.LongDiscriminator,
			})
		}

	case "device_command":
		var payload DeviceCommandPayload // Assumes DeviceCommandPayload is in models.go
		payloadBytes, _ := json.Marshal(msg.Payload)
		fmt.Println("msg Payload" , msg.Payload)
		if err := json.Unmarshal(payloadBytes, &payload); err != nil {
			client.notifyClientLog("command_response", "Invalid payload for device_command: "+err.Error())
			client.sendPayload("command_response", CommandResponsePayload{Success: false, Error: "Invalid payload: " + err.Error()}) // Assumes CommandResponsePayload is in models.go
			return
		}
		log.Printf("Handling device_command request: %+v", payload)
		if payload.NodeID == "" || payload.Cluster == "" || payload.Command == "" {
			client.sendPayload("command_response", CommandResponsePayload{Success: false, NodeID: payload.NodeID, Error: "Missing nodeId, cluster, or command"})
			return
		}
		endpointID := "1"
		cmdArgs := []string{strings.ToLower(payload.Cluster), strings.ToLower(payload.Command)}
		switch payload.Cluster {
		case "OnOff":
		case "LevelControl":
			if payload.Command == "MoveToLevel" {
				levelVal, okL := payload.Params["level"].(float64)
				ttVal, okTT := payload.Params["transitionTime"].(float64)
				if !okL {
					client.sendPayload("command_response", CommandResponsePayload{Success: false, NodeID: payload.NodeID, Error: "Missing or invalid 'level' parameter for MoveToLevel"})
					return
				}
				cmdArgs = append(cmdArgs, strconv.Itoa(int(levelVal)))
				if okTT {
					cmdArgs = append(cmdArgs, strconv.Itoa(int(ttVal)))
				} else {
					cmdArgs = append(cmdArgs, "0")
				}
				cmdArgs = append(cmdArgs, "0", "0")
			}
		default:
			for k, v := range payload.Params {
				client.notifyClientLog("command_response", fmt.Sprintf("Warning: Generic param handling for %s.%s - %s:%v. May not be correct for chip-tool.", payload.Cluster, payload.Command, k, v))
				cmdArgs = append(cmdArgs, fmt.Sprintf("%v", v))
			}
		}
		cmdArgs = append(cmdArgs, payload.NodeID, endpointID)
		cmd := exec.Command(chipToolPath, cmdArgs...) // Re-declare cmd
		client.notifyClientLog("command_response", fmt.Sprintf("Executing: %s %s", chipToolPath, strings.Join(cmdArgs, " ")))
		var outBuf, errBuf strings.Builder // Re-declare for this scope
		cmd.Stdout = &outBuf
		cmd.Stderr = &errBuf
		err := cmd.Run() // Re-declare err
		stdout := outBuf.String() // Re-declare
		stderr := errBuf.String() // Re-declare
		cmdOutput := fmt.Sprintf("Stdout:\n%s\nStderr:\n%s", stdout, stderr)
		log.Printf("chip-tool command output for %s.%s on %s:\n%s", payload.Cluster, payload.Command, payload.NodeID, cmdOutput)
		if err != nil {
			errMsg := fmt.Sprintf("Error executing %s.%s on %s: %v. Output: %s", payload.Command, payload.Cluster, payload.NodeID, err, cmdOutput)
			log.Println(errMsg)
			client.sendPayload("command_response", CommandResponsePayload{Success: false, NodeID: payload.NodeID, Error: errMsg, Details: cmdOutput})
			return
		}
		if strings.Contains(stdout, "CHIP Error") || strings.Contains(stderr, "CHIP Error") || strings.Contains(stderr, "Error:") {
			errMsg := "Command executed but chip-tool reported an error in its output."
			log.Println(errMsg, "Details:", cmdOutput)
			client.sendPayload("command_response", CommandResponsePayload{Success: false, NodeID: payload.NodeID, Error: errMsg, Details: cmdOutput})
		} else {
			log.Printf("Command %s.%s on Node %s executed. Output: %s", payload.Cluster, payload.Command, payload.NodeID, cmdOutput)
			client.sendPayload("command_response", CommandResponsePayload{Success: true, NodeID: payload.NodeID, Details: "Command executed. Output: " + cmdOutput})
			if payload.Cluster == "OnOff" && (payload.Command == "On" || payload.Command == "Off" || payload.Command == "Toggle") {
				go readAttribute(client, payload.NodeID, endpointID, "OnOff", "OnOff")
			}
			if payload.Cluster == "LevelControl" && payload.Command == "MoveToLevel" {
				go readAttribute(client, payload.NodeID, endpointID, "LevelControl", "CurrentLevel")
			}
		}
	case "get_status":
		payload, ok:= msg.Payload.(map[string]interface{})
		if !ok {
			log.Println("Invalid payload type for get_status")
			return
		}
		var statusPayload DeviceGetStatusPayload
		jsonPayload, _ := json.Marshal(payload)
		if err := json.Unmarshal(jsonPayload, &statusPayload); err != nil {
			log.Println("Failed to convert payload:", err)
			return
		}

		log.Println("Getting device status using chip-tool")
		cmd := exec.Command(chipToolPath, "onoff", "read", "on-off", statusPayload.deviceNodeId, statusPayload.deviceEndpointId)
		var outBuf_status, errBuf_status strings.Builder 
		cmd.Stdout = &outBuf_status
		cmd.Stderr = &errBuf_status
		err := cmd.Run()            
		stdout := outBuf_status.String()   
		stderr := errBuf_status.String()  

		re := regexp.MustCompile(`Data = (true|false)`)
		match := re.FindStringSubmatch(stdout)

		if err != nil && len(match)<1 {
			errMsg := fmt.Sprintf("Error getting the status of the device: %v. Output: %s", err, stderr)
			log.Println(errMsg)
			return
		}
		client.sendPayload("get_status", DeviceStatusPayload{
			nodeID: statusPayload.deviceNodeId,
			status: match[1],
		})
		break;
	case "subscribe_attribute":
		var payload SubscribeAttributePayload // Already defined globally in this file for the example
		payloadBytes, _ := json.Marshal(msg.Payload)
		if err := json.Unmarshal(payloadBytes, &payload); err != nil {
			client.notifyClientLog("subscription_log", "Invalid payload for subscribe_attribute: "+err.Error())
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
			epId = "1"
		}
		go startAttributeSubscription(client, payload.NodeID, epId, payload.Cluster, payload.Attribute, payload.MinInterval, payload.MaxInterval)

	default:
		log.Printf("Unknown message type from client %v: %s", client.conn.RemoteAddr(), msg.Type)
		client.notifyClient("error", map[string]interface{}{"message": "Unknown command type received: " + msg.Type})
	}
}


// Helper function to extract value after a known key (like "Hostname: ")
func extractValueAfterKey(line, key string) string {
	idx := strings.Index(line, key)
	if idx != -1 {
		// Value starts after the key string.
		valuePart := line[idx+len(key):]
		return strings.TrimSpace(valuePart)
	}
	return ""
}

// parseDiscoveryOutput parses the output of `chip-tool discover commissionables`
func parseDiscoveryOutput(output string, client *Client) []DiscoveredDevice { // DiscoveredDevice should be in models.go
	var devices []DiscoveredDevice
	var currentDevice *DiscoveredDevice

	scanner := bufio.NewScanner(strings.NewReader(output))

	for scanner.Scan() {
		rawLine := scanner.Text()
		strippedLine := stripAnsi(rawLine) // Remove ANSI codes first

		disMarker := "[DIS]"
		idxDis := strings.Index(strippedLine, disMarker)
		if idxDis == -1 {
			// client.notifyClientLog("discovery_log", "Skipping non-DIS line: '"+strippedLine+"'")
			continue
		}

		contentAfterDis := strings.TrimSpace(strippedLine[idxDis+len(disMarker):])
		if client != nil {
			client.notifyClientLog("discovery_log", "Processing content after [DIS]: '"+contentAfterDis+"'")
		}

		if strings.HasPrefix(contentAfterDis, "Discovered commissionable/commissioner node:") {
			if currentDevice != nil && (currentDevice.Discriminator != "" || currentDevice.InstanceName != "") {
				if currentDevice.ID == "" {
					if currentDevice.InstanceName != "" { currentDevice.ID = fmt.Sprintf("dnsd_instance_%s", currentDevice.InstanceName)
					} else { currentDevice.ID = fmt.Sprintf("dnsd_vid%s_pid%s_disc%s", currentDevice.VendorID, currentDevice.ProductID, currentDevice.Discriminator) }
				}
				if currentDevice.Name == "" {
					if currentDevice.InstanceName != "" { currentDevice.Name = fmt.Sprintf("MatterDevice-%s", currentDevice.InstanceName)
					} else if currentDevice.VendorID != "" && currentDevice.ProductID != "" { currentDevice.Name = fmt.Sprintf("MatterDevice-VID%s-PID%s", currentDevice.VendorID, currentDevice.ProductID)
					} else { currentDevice.Name = "Unknown Matter Device" }
				}
				devices = append(devices, *currentDevice)
				if client != nil {
					client.notifyClientLog("discovery_log", fmt.Sprintf("Completed parsing device: %+v", *currentDevice))
				}
			}
			currentDevice = &DiscoveredDevice{} 
			if client != nil {
				client.notifyClientLog("discovery_log", "New device block started by 'Discovered commissionable/commissioner node:'.")
			}
			continue 
		}

		if currentDevice != nil {
    var val string

    if val = extractValueAfterKey(contentAfterDis, "Hostname:"); val != "" {
        currentDevice.Name = val // Assign Hostname to Name as per your existing logic
        if client != nil {
            client.notifyClientLog("discovery_log", fmt.Sprintf("Parsed Hostname (as Name): %s", currentDevice.Name))
        }
    } else if val = extractValueAfterKey(contentAfterDis, "IP Address #1:"); val != "" {
        currentDevice.IPAddress = val // Assign to the new IPAddress field
        if client != nil {
            client.notifyClientLog("discovery_log", fmt.Sprintf("Parsed IP Address: %s", currentDevice.IPAddress))
        }
    } else if val = extractValueAfterKey(contentAfterDis, "Port:"); val != "" {
        if port, err := strconv.Atoi(val); err == nil {
            currentDevice.Port = port // Assign to the new Port field
            if client != nil {
                client.notifyClientLog("discovery_log", fmt.Sprintf("Parsed Port: %d", currentDevice.Port))
            }
        } else {
            if client != nil {
                client.notifyClientLog("discovery_log", fmt.Sprintf("Error parsing Port '%s': %v", val, err))
            }
        }
    } else if val = extractValueAfterKey(contentAfterDis, "Mrp Interval idle:"); val != "" {
        currentDevice.MrpIntervalIdle = val
        if client != nil {
            client.notifyClientLog("discovery_log", fmt.Sprintf("Parsed Mrp Interval idle: %s", currentDevice.MrpIntervalIdle))
        }
    } else if val = extractValueAfterKey(contentAfterDis, "Mrp Interval active:"); val != "" {
        currentDevice.MrpIntervalActive = val
        if client != nil {
            client.notifyClientLog("discovery_log", fmt.Sprintf("Parsed Mrp Interval active: %s", currentDevice.MrpIntervalActive))
        }
    } else if val = extractValueAfterKey(contentAfterDis, "Mrp Active Threshold:"); val != "" {
        currentDevice.MrpActiveThreshold = val
        if client != nil {
            client.notifyClientLog("discovery_log", fmt.Sprintf("Parsed Mrp Active Threshold: %s", currentDevice.MrpActiveThreshold))
        }
    } else if val = extractValueAfterKey(contentAfterDis, "TCP Client Supported:"); val != "" {
        // Assuming 0 or 1. Convert to bool.
        currentDevice.TCPClientSupported = (val == "1")
        if client != nil {
            client.notifyClientLog("discovery_log", fmt.Sprintf("Parsed TCP Client Supported: %t", currentDevice.TCPClientSupported))
        }
    } else if val = extractValueAfterKey(contentAfterDis, "TCP Server Supported:"); val != "" {
        // Assuming 0 or 1. Convert to bool.
        currentDevice.TCPServerSupported = (val == "1")
        if client != nil {
            client.notifyClientLog("discovery_log", fmt.Sprintf("Parsed TCP Server Supported: %t", currentDevice.TCPServerSupported))
        }
    } else if val = extractValueAfterKey(contentAfterDis, "ICD:"); val != "" {
        currentDevice.ICD = val
        if client != nil {
            client.notifyClientLog("discovery_log", fmt.Sprintf("Parsed ICD: %s", currentDevice.ICD))
        }
    } else if val = extractValueAfterKey(contentAfterDis, "Vendor ID:"); val != "" {
        currentDevice.VendorID = val // Still a string as per updated struct
        if client != nil {
            client.notifyClientLog("discovery_log", fmt.Sprintf("Parsed Vendor ID: %s", currentDevice.VendorID))
        }
    } else if val = extractValueAfterKey(contentAfterDis, "Product ID:"); val != "" {
        currentDevice.ProductID = val // Still a string as per updated struct
        if client != nil {
            client.notifyClientLog("discovery_log", fmt.Sprintf("Parsed Product ID: %s", currentDevice.ProductID))
        }
    } else if val = extractValueAfterKey(contentAfterDis, "Long Discriminator:"); val != "" {
        currentDevice.Discriminator = val // Still a string as per updated struct
        if client != nil {
            client.notifyClientLog("discovery_log", fmt.Sprintf("Parsed Long Discriminator: %s", currentDevice.Discriminator))
        }
    } else if val = extractValueAfterKey(contentAfterDis, "Pairing Hint:"); val != "" {
        if ph, err := strconv.ParseUint(val, 10, 16); err == nil {
            currentDevice.PairingHint = uint16(ph)
            if client != nil {
                client.notifyClientLog("discovery_log", fmt.Sprintf("Parsed Pairing Hint: %d", currentDevice.PairingHint))
            }
        } else {
            if client != nil {
                client.notifyClientLog("discovery_log", fmt.Sprintf("Error parsing Pairing Hint '%s': %v", val, err))
            }
        }
    } else if val = extractValueAfterKey(contentAfterDis, "Instance Name:"); val != "" {
        currentDevice.InstanceName = val
        if client != nil {
            client.notifyClientLog("discovery_log", fmt.Sprintf("Parsed Instance Name: %s", currentDevice.InstanceName))
        }
    } else if val = extractValueAfterKey(contentAfterDis, "Commissioning Mode:"); val != "" {
        if cm, err := strconv.ParseUint(val, 10, 8); err == nil {
            currentDevice.CommissioningMode = uint8(cm)
            switch currentDevice.CommissioningMode {
            case 1:
                currentDevice.Type = "BLE"
            case 2:
                currentDevice.Type = "OnNetwork (DNS-SD)"
            default:
                currentDevice.Type = fmt.Sprintf("CM:%d", currentDevice.CommissioningMode)
            }
            if client != nil {
                client.notifyClientLog("discovery_log", fmt.Sprintf("Parsed Commissioning Mode: %d (Type: %s)", currentDevice.CommissioningMode, currentDevice.Type))
            }
        } else {
            if client != nil {
                client.notifyClientLog("discovery_log", fmt.Sprintf("Error parsing Commissioning Mode '%s': %v", val, err))
            }
        }
    } else if val = extractValueAfterKey(contentAfterDis, "Supports Commissioner Generated Passcode:"); val != "" {
        // Convert "true" or "false" string to boolean
        currentDevice.SupportsCommissionerGeneratedPasscode = (val == "true")
        if client != nil {
            client.notifyClientLog("discovery_log", fmt.Sprintf("Parsed Supports Commissioner Generated Passcode: %t", currentDevice.SupportsCommissionerGeneratedPasscode))
        }
    }
}
	}

	if currentDevice != nil && (currentDevice.Discriminator != "" || currentDevice.InstanceName != "") {
		if currentDevice.ID == "" {
			if currentDevice.InstanceName != "" { currentDevice.ID = fmt.Sprintf("dnsd_instance_%s", currentDevice.InstanceName)
			} else { currentDevice.ID = fmt.Sprintf("dnsd_vid%s_pid%s_disc%s", currentDevice.VendorID, currentDevice.ProductID, currentDevice.Discriminator) }
		}
		if currentDevice.Name == "" {
			if currentDevice.InstanceName != "" { currentDevice.Name = fmt.Sprintf("MatterDevice-%s", currentDevice.InstanceName)
			} else if currentDevice.VendorID != "" && currentDevice.ProductID != "" { currentDevice.Name = fmt.Sprintf("MatterDevice-VID%s-PID%s", currentDevice.VendorID, currentDevice.ProductID)
			} else { currentDevice.Name = "Unknown Matter Device" }
		}
		devices = append(devices, *currentDevice)
		if client != nil {
			client.notifyClientLog("discovery_log", fmt.Sprintf("Completed parsing final device: %+v", *currentDevice))
		}
	}

	if client != nil { 
		if len(devices) == 0 {
			client.notifyClientLog("discovery_log", "No devices parsed from output. Check chip-tool output and parsing logic. Final output scan complete.")
		} else {
			client.notifyClientLog("discovery_log", fmt.Sprintf("Successfully parsed %d device(s).", len(devices)))
		}
	}
	return devices
}

func (c *Client) notifyClientLog(logType string, data string) {
	msg := ServerMessage{Type: logType, Payload: data} // ServerMessage should be in models.go
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

func (c *Client) notifyClient(msgType string, payload interface{}) {
	msg := ServerMessage{Type: msgType, Payload: payload} // ServerMessage should be in models.go
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

func (c *Client) sendPayload(msgType string, payload interface{}) {
	c.notifyClient(msgType, payload)
}

func readAttribute(client *Client, nodeID, endpointID, clusterName, attributeName string) {
	log.Printf("Attempting to read attribute %s.%s for Node %s Endpoint %s", clusterName, attributeName, nodeID, endpointID)
	client.notifyClientLog("commissioning_log", fmt.Sprintf("Reading attribute %s.%s for Node %s...", clusterName, attributeName, nodeID))
	cmdArgs := []string{strings.ToLower(clusterName), "read", attributeName, nodeID, endpointID} // Attribute name often PascalCase for chip-tool read
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

	var value interface{}
	parsed := false
	reValue := regexp.MustCompile(`CHIP:DMG: Value = (.*)`)
	matches := reValue.FindStringSubmatch(stdout)

	if len(matches) > 1 {
		valStr := strings.TrimSpace(matches[1])
		if bVal, err := strconv.ParseBool(valStr); err == nil {
			value = bVal
			parsed = true
		} else if iVal, err := strconv.ParseInt(valStr, 10, 64); err == nil {
			value = iVal
			parsed = true
		} else if fVal, err := strconv.ParseFloat(valStr, 64); err == nil {
			value = fVal
			parsed = true
		} else {
			if strings.HasPrefix(valStr, `"`) && strings.HasSuffix(valStr, `"`) {
				value = strings.Trim(valStr, `"`)
			} else {
				value = valStr
			}
			parsed = true
		}
	}
	if !parsed {
		log.Printf("Could not parse value for attribute %s.%s from output: %s", clusterName, attributeName, stdout)
		client.notifyClientLog("commissioning_log", fmt.Sprintf("Could not parse value for %s.%s", clusterName, attributeName))
		value = "Raw: " + stdout
	}
	log.Printf("Attribute %s.%s for Node %s read. Value: %v (Parsed: %t)", clusterName, attributeName, nodeID, value, parsed)
	client.sendPayload("attribute_update", AttributeUpdatePayload{ // Assumes AttributeUpdatePayload is in models.go
		NodeID:    nodeID, EndpointID: endpointID, Cluster: clusterName, Attribute: attributeName, Value: value,
	})
}

func startAttributeSubscription(client *Client, nodeID, endpointID, clusterName, attributeName, minInterval, maxInterval string) {
	subscriptionID := fmt.Sprintf("sub-%s-%s-%s-%s", nodeID, endpointID, clusterName, attributeName)
	log.Printf("[%s] Starting subscription for Node %s, Endpoint %s, Cluster %s, Attribute %s, MinInterval %ss, MaxInterval %ss",
		subscriptionID, nodeID, endpointID, clusterName, attributeName, minInterval, maxInterval)

	client.notifyClientLog("subscription_log", fmt.Sprintf("Attempting to subscribe to %s/%s on Node %s EP%s", clusterName, attributeName, nodeID, endpointID))

	cmdArgs := []string{
		strings.ToLower(clusterName), "subscribe", attributeName, minInterval, maxInterval, nodeID, endpointID,
	}
	cmd := exec.Command(chipToolPath, cmdArgs...)

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

	go func() { // Stderr
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
	go func() { // Stdout
		scanner := bufio.NewScanner(stdoutPipe)
		reDataLine := regexp.MustCompile(`CHIP:DMG:\s+Data = (.*) \((.*)\)`)
		reReportStart := regexp.MustCompile(`CHIP:DMG: ReportDataMessage =`)
		inReportBlock := false
		for scanner.Scan() {
			line := scanner.Text()
			log.Printf("[%s] Stdout: %s", subscriptionID, line)
			if reReportStart.MatchString(line) {
				inReportBlock = true
				log.Printf("[%s] Detected report start.", subscriptionID)
				continue
			}
			if inReportBlock {
				if matches := reDataLine.FindStringSubmatch(line); len(matches) == 3 {
					valStr := strings.TrimSpace(matches[1])
					typeStr := strings.TrimSpace(matches[2])
					var value interface{}
					var parseErr error
					switch typeStr {
					case "BOOLEAN":
						value, parseErr = strconv.ParseBool(valStr)
					case "INT8S", "INT16S", "INT32S", "INT64S", "UINT8", "UINT16", "UINT32", "UINT64", "INT8U", "INT16U", "INT32U", "INT64U":
						value, parseErr = strconv.ParseInt(valStr, 10, 64)
					case "FLOAT", "DOUBLE":
						value, parseErr = strconv.ParseFloat(valStr, 64)
					case "UTF8S", "OCTET_STRING":
						if strings.HasPrefix(valStr, `"`) && strings.HasSuffix(valStr, `"`) {
							value = strings.Trim(valStr, `"`)
						} else {
							value = valStr
						}
					default:
						log.Printf("[%s] Unhandled data type from subscription: %s.", subscriptionID, typeStr)
						value = valStr
					}
					if parseErr != nil {
						log.Printf("[%s] Error parsing value '%s' as type '%s': %v.", subscriptionID, valStr, typeStr, parseErr)
						value = valStr
					}
					client.sendPayload("attribute_update", AttributeUpdatePayload{NodeID: nodeID, EndpointID: endpointID, Cluster: clusterName, Attribute: attributeName, Value: value}) // Assumes AttributeUpdatePayload is in models.go
					inReportBlock = false
				} else if strings.Contains(line, "CHIP:DMG: }") {
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
		waitErr := cmd.Wait()
		log.Printf("[%s] chip-tool subscribe command finished. Exit error: %v", subscriptionID, waitErr)
		client.notifyClientLog("subscription_log", fmt.Sprintf("Subscription for %s/%s on Node %s ended. Error: %v", clusterName, attributeName, nodeID, waitErr))
	}()
}
