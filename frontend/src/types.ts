// src/types.ts

// Represents a command to be executed on the Raspberry Pi for hub setup
export interface HubCommand {
  id: number
  instruction: string
  command: string
  expectedOutput: string
  confirmed: boolean
}

// Represents a discovered Matter device before commissioning
export interface DiscoveredDevice {
  id: string // Unique identifier, e.g., derived from discriminator or MAC
  name?: string // Name of the device, if available from discovery
  type?: string // e.g., "light", "sensor"
  discriminator: string // Matter device discriminator
  vendorId?: string // Vendor ID
  productId?: string // Product ID
  nodeId?: number | string // Assigned Matter Node ID after commissioning
  status?: string
  // Add any other relevant fields from chip-tool discovery output
}

// Represents the status of a device attribute
// e.g., { OnOff_OnOff: true, LevelControl_CurrentLevel: 128 }
export interface DeviceAttributeStatus {
  [attributeKey: string]: any
}

// Represents the overall status of all attributes for a given Node ID
export interface DeviceNodeStatus {
  [nodeId: string]: DeviceAttributeStatus
}

// Message structure for WebSocket communication (client to server)
export interface ClientToServerMessage {
  type: string
  payload?: any
}

// Message structure for WebSocket communication (server to client)
export interface ServerToClientMessage {
  type: string
  payload?: any
  data?: any // Some of your existing messages use 'data'
  // Specific payload types for better type safety
  // Example for discovery_result:
  // payload?: { devices: DiscoveredDevice[]; error?: string };
  // Example for commissioning_status:
  // payload?: { success: boolean; nodeId?: number | string; details?: string; error?: string; discriminatorAssociatedWithRequest?: string };
  // Example for attribute_update:
  // payload?: { nodeId: string; endpointId?: string; cluster: string; attribute: string; value: any };
}

// Specific payload types for server messages
export interface DiscoveryResultPayload {
  devices: DiscoveredDevice[]
  error?: string
}

export interface deviceStatusPayload {
  nodeId: string
  status: string
}

export interface CommissioningStatusPayload {
  success: boolean
  nodeId: string
  details?: string
  error?: string
  discriminatorAssociatedWithRequest?: string // To map back to the discovered device
  EndpointID?: string
  // Include other fields backend might send like the original discriminator
  originalDiscriminator: string
}

export interface AttributeUpdatePayload {
  nodeId: string | number // Node ID can be number or string
  endpointId?: string | number
  cluster: string
  attribute: string
  value: any
}

export interface CommandResponsePayload {
  success: boolean
  nodeId?: string | number
  details?: string
  error?: string
}

// Extending ServerToClientMessage for more specific payloads
export type TypedServerToClientMessage =
  | ServerToClientMessage // Generic for less typed messages
  | ({ type: 'discovery_result' } & { payload: DiscoveryResultPayload })
  | ({ type: 'commissioning_status' } & { payload: CommissioningStatusPayload })
  | ({ type: 'attribute_update' } & { payload: AttributeUpdatePayload })
  | ({ type: 'command_response' } & { payload: CommandResponsePayload })
  | ({ type: 'discovery_log' } & { payload: string })
  | ({ type: 'commissioning_log' } & { payload: string })
  | ({ type: 'hub_setup_log' } & { payload: string })
  | ({ type: 'internal_log' } & { data: string })
  | ({ type: 'internal_error' } & { data: string })
  | ({ type: 'error' } & { payload: { message: string } })
