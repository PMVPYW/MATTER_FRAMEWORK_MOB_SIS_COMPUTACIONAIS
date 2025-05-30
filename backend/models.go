package main

// ClientMessage represents a message received from the WebSocket client (Vue frontend)
type ClientMessage struct {
	Type    string      `json:"type"`              // e.g., "discover_devices", "commission_device", "device_command"
	Payload interface{} `json:"payload,omitempty"` // Flexible payload based on message type
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
	ID            string `json:"id"`                       // Unique identifier for the frontend
	Name          string `json:"name,omitempty"`           // Name of the device
	Type          string `json:"type,omitempty"`           // e.g., "light", "sensor" (frontend might derive this)
	Discriminator string `json:"discriminator"`            // Matter device discriminator
	VendorID      string `json:"vendorId,omitempty"`       // Vendor ID
	ProductID     string `json:"productId,omitempty"`      // Product ID
	NodeID        string `json:"nodeId,omitempty"`         // Assigned Matter Node ID after commissioning (can be string or int, frontend expects string or number)
	MACAddress    string `json:"macAddress,omitempty"`     // MAC address if available from discovery (useful for unique ID)
	PairingHint   uint16 `json:"pairingHint,omitempty"`    // Pairing hint if available
	DeviceType    uint32 `json:"deviceType,omitempty"`     // Matter device type code
	CommissioningMode uint8 `json:"commissioningMode,omitempty"` // Commissioning mode
	InstanceName  string `json:"instanceName,omitempty"` // Instance name (often from DNS-SD)

	// Add other relevant fields from chip-tool discovery output as needed
}

// CommissionDevicePayload is the expected structure for "commission_device" message from client
type CommissionDevicePayload struct {
	Discriminator  string `json:"discriminator"`
	SetupCode      string `json:"setupCode"`
	NodeIDToAssign string `json:"nodeIdToAssign"` // This is the temporary/proposed Node ID from frontend
	VendorID       string `json:"vendorId,omitempty"`
	ProductID      string `json:"productId,omitempty"`
}

// DeviceCommandPayload is the expected structure for "device_command" message from client
type DeviceCommandPayload struct {
	NodeID  string                 `json:"nodeId"`  // Node ID of the device to control
	Cluster string                 `json:"cluster"` // e.g., "OnOff", "LevelControl"
	Command string                 `json:"command"` // e.g., "On", "Off", "MoveToLevel"
	Params  map[string]interface{} `json:"params,omitempty"` // Command-specific parameters
}

// CommissioningStatusPayload is sent to the client after a commissioning attempt
type CommissioningStatusPayload struct {
	Success                        bool   `json:"success"`
	NodeID                         string `json:"nodeId,omitempty"` // The actual Node ID assigned by the Matter fabric
	Details                        string `json:"details,omitempty"`
	Error                          string `json:"error,omitempty"`
	OriginalDiscriminator          string `json:"originalDiscriminator,omitempty"` // Helps frontend map back
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
