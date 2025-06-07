package main

// ClientMessage represents a message received from the WebSocket client (Vue frontend)
type ClientMessage struct {
	Type    string      `json:"type"`              // e.g., "discover_devices", "commission_device", "device_command"
	Payload interface{} `json:"payload,omitempty"` // Flexible payload based on message type
}

type ClientMessageGetStatus struct {
	Type    string      `json:"type"`              // e.g., "discover_devices", "commission_device", "device_command"
	Payload DeviceGetStatusPayload `json:"payload,omitempty"` // Flexible payload based on message type
}
type DeviceGetStatusPayload struct {
    deviceNodeId     string `json:"deviceNodeId"`
    deviceEndpointId string `json:"deviceEndpointId"`
}


// ServerMessage represents a message sent to the WebSocket client (Vue frontend)
type ServerMessage struct {
	Type    string      `json:"type"`              // e.g., "discovery_result", "commissioning_status", "attribute_update", "log"
	Payload interface{} `json:"payload,omitempty"` // Flexible payload
	Data    interface{} `json:"data,omitempty"`    // Alternative field for payload, matching frontend's internal_log/error
}

// DiscoveredDevice represents information about a device found during discovery
// This should align with the frontend's `DiscoveredDevice` type in `types.ts`
type DiscoveredDevice struct {
    ID                              string `json:"id"`                       // Unique identifier for the frontend
    Name                            string `json:"name,omitempty"`           // Name of the device (often maps to Hostname)
    Type                            string `json:"type,omitempty"`           // e.g., "BLE", "OnNetwork (DNS-SD)" derived from CommissioningMode
    IPAddress                       string `json:"ipAddress,omitempty"`      // IP Address #1
    Port                            int    `json:"port,omitempty"`           // Port
    MrpIntervalIdle                 string `json:"mrpIntervalIdle,omitempty"`    // Mrp Interval idle (e.g., "not present")
    MrpIntervalActive               string `json:"mrpIntervalActive,omitempty"`  // Mrp Interval active (e.g., "not present")
    MrpActiveThreshold              string `json:"mrpActiveThreshold,omitempty"` // Mrp Active Threshold (e.g., "not present")
    TCPClientSupported              bool   `json:"tcpClientSupported,omitempty"` // TCP Client Supported (0 or 1, converted to bool)
    TCPServerSupported              bool   `json:"tcpServerSupported,omitempty"` // TCP Server Supported (0 or 1, converted to bool)
    ICD                             string `json:"icd,omitempty"`                // ICD (e.g., "not present")
    Discriminator                   string `json:"discriminator"`            // Long Discriminator
    VendorID                        string `json:"vendorId,omitempty"`       // Vendor ID
    ProductID                       string `json:"productId,omitempty"`      // Product ID
    NodeID                          string `json:"nodeId,omitempty"`         // Assigned Matter Node ID after commissioning (can be string or int)
    MACAddress                      string `json:"macAddress,omitempty"`     // MAC address if available from discovery (not in provided logs, but good to keep if needed)
    PairingHint                     uint16 `json:"pairingHint,omitempty"`    // Pairing hint
    DeviceType                      uint32 `json:"deviceType,omitempty"`     // Matter device type code (not in provided logs, but common in discovery)
    CommissioningMode               uint8  `json:"commissioningMode,omitempty"` // Commissioning mode
    InstanceName                    string `json:"instanceName,omitempty"` // Instance name (often from DNS-SD)
    SupportsCommissionerGeneratedPasscode bool `json:"supportsCommissionerGeneratedPasscode,omitempty"` // Supports Commissioner Generated Passcode
}

// CommissionDevicePayload is the expected structure for "commission_device" message from client
type CommissionDevicePayload struct {
	SetupCode                             string `json:"setupCode"`
    Hostname                              string `json:"hostname"`
    IPAddress                             string `json:"ipAddress"`
    Port                                  string `json:"port"` 
    MrpIntervalIdle                       string `json:"mrpIntervalIdle,omitempty"`    // Using string as "not present" is a value
    MrpIntervalActive                     string `json:"mrpIntervalActive,omitempty"`  // Using string as "not present" is a value
    MrpActiveThreshold                    string `json:"mrpActiveThreshold,omitempty"` // Using string as "not present" is a value
    TCPClientSupported                    string `json:"tcpClientSupported"`
    TCPServerSupported                    string `json:"tcpServerSupported"`
    ICD                                   string `json:"icd,omitempty"`              // Using string as "not present" is a value
    VendorID                              string `json:"vendorId"`
    ProductID                             string `json:"productId"`
    LongDiscriminator                     string `json:"discriminator"`
    PairingHint                           string `json:"pairingHint"`
    InstanceName                          string `json:"instanceName"`
    CommissioningMode                     string `json:"commissioningMode"`
    NodeID                                string `json:"nodeid"`
    SupportsCommissionerGeneratedPasscode string `json:"supportsCommissionerGeneratedPasscode"`
    EndpointId                            string `json:"endpointId"`
}

// DeviceCommandPayload is the expected structure for "device_command" message from client
type DeviceCommandPayload struct {
	NodeID  string                 `json:"nodeId"`  // Node ID of the device to control
	Cluster string                 `json:"cluster"` // e.g., "OnOff", "LevelControl"
	Command string                 `json:"command"` // e.g., "On", "Off", "MoveToLevel"
	Params  map[string]interface{} `json:"params,omitempty"` // Command-specific parameters
}

type DeviceStatusPayload struct {
	nodeID  string                 `json:"nodeId"`  // Node ID of the device to control
	status string                 `json:"status"` // e.g., "online", "offline", "error"
}

// CommissioningStatusPayload is sent to the client after a commissioning attempt
type CommissioningStatusPayload struct {
	Success                        bool   `json:"success"`
	NodeID                         string `json:"nodeId,omitempty"` // The actual Node ID assigned by the Matter fabric
	Details                        string `json:"details,omitempty"`
	Error                          string `json:"error,omitempty"`
	OriginalDiscriminator          string `json:"originalDiscriminator,omitempty"` // Helps frontend map back
    EndpointId                     string `json:"endpointId"`
	DiscriminatorAssociatedWithRequest string `json:"discriminatorAssociatedWithRequest,omitempty"` // From client request
}

// AttributeUpdatePayload is sent to the client when a device attribute changes
type AttributeUpdatePayload struct {
	NodeID     string      `json:"nodeId"`
	EndpointID string      `json:"endpointId,omitempty"` // Typically "1" for simple devices
	Cluster    string      `json:"cluster"`
	Attribute  string      `json:"attribute"`
	Value      interface{} `json:"value"`
}

// CommandResponsePayload is sent to the client after a device command attempt
type CommandResponsePayload struct {
	Success bool   `json:"success"`
	NodeID  string `json:"nodeId,omitempty"`
	Details string `json:"details,omitempty"`
	Error   string `json:"error,omitempty"`
}

// DiscoveryResultPayload is sent to the client after a device discovery scan
type DiscoveryResultPayload struct {
	Devices []DiscoveredDevice `json:"devices"`
	Error   string             `json:"error,omitempty"`
}
