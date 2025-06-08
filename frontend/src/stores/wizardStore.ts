import { defineStore } from 'pinia'
import { ref, computed } from 'vue' // Runtime imports
import type { Ref, ComputedRef } from 'vue' // Type-only imports
import { sendMessage } from '@/services/websocketService'
import type {
  HubCommand,
  DiscoveredDevice,
  DeviceNodeStatus,
  TypedServerToClientMessage,
  DiscoveryResultPayload,
  CommissioningStatusPayload,
  AttributeUpdatePayload,
  CommandResponsePayload,
  deviceStatusPayload,
} from '@/types'

// Define the state structure (already defined in previous version, ensure it's consistent)
export interface WizardState {
  currentStep: number
  totalSteps: number
  rpiIpAddress: string
  hubSetupCommands: HubCommand[]
  hubSetupLogs: string[]
  discoveredDevices: DiscoveredDevice[]
  selectedDevice: DiscoveredDevice | null
  commissioningLogs: string[]
  deviceStatus: DeviceNodeStatus
}

export const useWizardStore = defineStore('wizard', () => {
  // --- State ---
  const currentStep: Ref<number> = ref(1)
  const totalSteps: Ref<number> = ref(3)
  const rpiIpAddress: Ref<string> = ref('')

  const hubSetupCommands: Ref<HubCommand[]> = ref([
    // ... (commands remain the same as in the previous version: matter_frontend_wizard_store_ts)
    {
      id: 1,
      instruction: 'Update package list and install dependencies.',
      command:
        'sudo apt update && sudo apt install -y git python3-pip build-essential libglib2.0-dev-bin libglib2.0-dev libssl-dev libffi-dev python3-dev libavahi-client-dev python3.10-venv avahi-daemon bluez',
      expectedOutput: 'Packages updated and dependencies installed.',
      confirmed: false,
            tooltip: "Prepares your Raspberry Pi by installing essential software. These packages are required to download, compile, and run the Matter SDK, which depends on tools like Git, Python, and Avahi for network device discovery."

    },
    // {
    //   id: 2,
    //   instruction: 'Clone the Matter SDK (chip-project/connectedhomeip).',
    //   command: 'git clone https://github.com/project-chip/connectedhomeip.git ~/connectedhomeip',
    //   expectedOutput: 'Repository cloned successfully.',
    //   confirmed: false,
    //         tooltip: "Downloads the complete source code for the Matter protocol from its official GitHub repository. This is a mandatory step to get the code needed to build the controller and tools on your device."

    // },
    // {
    //   id: 3,
    //   instruction: 'Bootstrap the Matter SDK environment.',
    //   command: 'cd ~/connectedhomeip && chmod 777 ./scripts/bootstrap.sh && ./scripts/bootstrap.sh',
    //   expectedOutput: 'Environment bootstrapped.',
    //   confirmed: false,
    //         tooltip: "Runs a setup script from the Matter SDK. This script automatically downloads and configures specific tools required by the SDK, ensuring a consistent and correct build environment."

    // },
    // {
    //   id: 4,
    //   instruction: 'Activate the Matter SDK environment.',
    //   command:
    //     'cd ~/connectedhomeip && chmod 777 ./scripts/activate.sh && source ./scripts/activate.sh',
    //   expectedOutput: 'Environment activated (prompt might change).',
    //   confirmed: false,
    //   tooltip: "Activates the Matter development environment in your current terminal session. This is crucial because it tells the system where to find the specific compilers and tools installed by the SDK, making them available for the next commands."
    // },
    // {
    //   id: 5,
    //   instruction: 'Clone submodules',
    //   command:
    //     'cd ~/connectedhomeip && git submodule deinit -f . && git submodule update --init --recursive',
    //   expectedOutput:
    //     'This command produces no output, but you should wait for it to complete to be sure',
    //   confirmed: false,
    //         tooltip: "Downloads all the third-party libraries and projects that the Matter SDK depends on (e.g., for cryptography). This is necessary because the main repository doesn't contain all the code; it links to external projects, and this command fetches them."


    // },
    // {
    //   id: 6,
    //   instruction: 'Build the chip-tool (this can take a long time).',
    //   command:
    //     'cd ~/connectedhomeip && ./scripts/examples/gn_build_example.sh examples/chip-tool out/chip-tool-arm64',
    //   expectedOutput: 'chip-tool built successfully.',
    //   confirmed: false,
    //             tooltip: "This command compiles all the source code to create the 'chip-tool'. This tool is a powerful command-line application that is essential for commissioning (adding) and controlling your Matter devices from the Raspberry Pi."


    // },
    {
      id: 2,
      instruction: 'Install chip-tool via snap.',
        command:
          'sudo snap install chip-tool',
        expectedOutput: 'chip-tool <version number> from Canonical IoT Labs✓ installed',
        confirmed: false,
                  tooltip: "This command uses Snap, to install the chip-tool. This method is a convenient and fast alternative to building the tool from source code, as Snap packages the application with all its dependencies."
      },
    {
      id: 3,
      instruction: 'Optional: Change firewall settings (only if you intend to use a virtual Matter device on another computer).',
        command:
          'sudo ufw allow 5353/udp & sudo ufw allow 5540/udp',
        expectedOutput: 'Rule added (v6)...',
        confirmed: false,
                  tooltip: "This opens two essential firewall ports for a virtual Matter device. Port 5353/udp (mDNS) is required for the device to be discovered and commissioned on your network. Port 5540/udp is used for operational commands, such as turning the virtual device on or off."
      },
    // {
    //   id: 6,
    //   instruction: 'Optional: Install OpenThread Border Router (OTBR) if using Thread devices.',
    //   command:
    //     '# Placeholder: Refer to official OTBR setup guides for Raspberry Pi. \n# Example: git clone https://github.com/openthread/ot-br-posix.git ... then build and install.',
    //   expectedOutput: 'OTBR installed and running.',
    //   confirmed: false,
    // },
  ])

  const hubSetupLogs: Ref<string[]> = ref([])
  const discoveredDevices: Ref<DiscoveredDevice[]> = ref([])
  const selectedDevice: Ref<DiscoveredDevice | null> = ref(null)
  const commissioningLogs: Ref<string[]> = ref([])
  const valueLogs: Ref<string[]> = ref([])
  const comunicationLogs: Ref<string[]> = ref([])
  const deviceStatus: Ref<DeviceNodeStatus> = ref({})

  // --- Getters ---
  const progress: ComputedRef<number> = computed(() => (currentStep.value / totalSteps.value) * 100)
  const isHubSetupComplete: ComputedRef<boolean> = computed(() =>
    hubSetupCommands.value.every((cmd) => cmd.confirmed),
  )

  // --- Actions ---
  function nextStep(): void {
    if (currentStep.value < totalSteps.value) {
      currentStep.value++
    }
  }

  function previousStep(): void {
    if (currentStep.value > 1) {
      currentStep.value--
    }
  }

  function setRpiIp(ip: string): void {
    rpiIpAddress.value = ip
    console.log('RPi IP set to:', ip)
  }

  function confirmCommand(commandId: number): void {
    const command = hubSetupCommands.value.find((cmd) => cmd.id === commandId)
    if (command) {
      command.confirmed = true
      hubSetupLogs.value.push(`User confirmed execution of: ${command.command.split('\n')[0]}...`)
    }
  }

  function startDeviceDiscovery(): void {
    discoveredDevices.value = []
    commissioningLogs.value.push('Attempting to start device discovery via backend...')
    sendMessage({ type: 'discover_devices' })
  }

  function getDeviceStatus(deviceNodeId: string, deviceEndpointId: string) {
    console.log(`Getting status from device ${deviceNodeId} on endpoint ${deviceEndpointId}`)
    sendDeviceCommand(deviceNodeId, 'OnOff', 'read', {
      attribute: 'on-off',
      endpointId: deviceEndpointId,
    })
  }

  function commissionDevice(device: DiscoveredDevice): void {
    commissioningLogs.value.push(
      `Attempting to commission ${device.name || `Device with Discriminator: ${device.discriminator}`}...`,
    )

    const setupCode = prompt(
      `Enter setup code for ${device.name || device.discriminator} (e.g., MT:XXXXXXXXXXX or numeric):`,
    )
    if (setupCode) {
      const tempNodeIdToAssign = Date.now() % 100000

      sendMessage({
        type: 'commission_device',
        payload: {
          discriminator: device.discriminator,
          setupCode: setupCode,
          nodeIdToAssign: String(tempNodeIdToAssign),
          vendorId: device.vendorId,
          productId: device.productId,
        },
      })
      commissioningLogs.value.push(
        `Sent commissioning request for discriminator ${device.discriminator} with setup code and temp NodeID ${tempNodeIdToAssign}.`,
      )
    } else {
      commissioningLogs.value.push('Commissioning cancelled: No setup code provided.')
    }
  }

  function sendDeviceCommand(
    nodeId: string | number,
    cluster: string,
    command: string,
    params: Record<string, any> = {},
  ): void {
    if (!nodeId) {
      commissioningLogs.value.push('Error: Cannot send command. No Node ID for selected device.')
      alert('Error: Device does not have a Node ID. Has it been commissioned successfully?')
      return
    }
    commissioningLogs.value.push(
      `Sending command to Node ID ${nodeId}: ${cluster}.${command} with payload ${JSON.stringify(params)}`,
    )
    sendMessage({
      type: 'device_command',
      payload: {
        nodeId: nodeId,
        cluster: cluster,
        command: command,
        params: params,
      },
    })
  }

  function handleBackendMessage(message: TypedServerToClientMessage): void {
    console.log('Message from backend:', message)

    switch (message.type) {
      case 'hub_setup_log':
        // Ensure payload is treated as string for these log types if defined in TypedServerToClientMessage
        hubSetupLogs.value.push(`[Backend Hub Log]: ${message.payload as string}`)
        break
      case 'discovery_log':
        commissioningLogs.value.push(`[Discovery Log]: ${message.payload as string}`)
        break
      // case 'get_status':
      //   const deviceStatusPayload = message.payload as deviceStatusPayload
      //   if (deviceStatusPayload) {
      //     const currentDeviceIndex = discoveredDevices.value.findIndex(
      //       (device) => String(device.nodeId) === deviceStatusPayload.nodeId,
      //     )
      //     if (currentDeviceIndex < 0) return
      //     discoveredDevices.value[currentDeviceIndex].status = deviceStatusPayload.status
      //   } else {
      //     console.log('oh nooo... la polizia....')
      //   }
      //   break
      case 'discovery_result':
        const discoveryPayload = message.payload as DiscoveryResultPayload
        if (discoveryPayload && Array.isArray(discoveryPayload.devices)) {
          discoveredDevices.value = discoveryPayload.devices.map((d) => ({
            ...d,
            id: d.id || `device_${d.discriminator}_${d.vendorId}_${d.productId}`,
          }))
          console.log('discoveredDevices', discoveredDevices.value)
          console.log('discoveredDevices - message.payload', message.payload)
        } else {
          discoveredDevices.value = []
        }
        commissioningLogs.value.push(
          `Device discovery results received. Found: ${discoveredDevices.value.length} devices. ${discoveryPayload.error ? 'Error: ' + discoveryPayload.error : ''}`,
        )
        break
      case 'commissioning_log':
        commissioningLogs.value.push(`[Commissioning Log]: ${message.payload as string}`)
        break
      case 'commissioning_status':
        var statusPayload = message.payload as CommissioningStatusPayload

        console.log('statusPayload', statusPayload)
        console.log('message.payload', message.payload)
        commissioningLogs.value.push(
          `[Commissioning Status]: Success: ${statusPayload.success}, Node ID: ${statusPayload.nodeId}, Details: ${statusPayload.details || statusPayload.error || ''}`,
        )
        console.log('CommissioningLogs', commissioningLogs.value)
        localStorage.setItem(statusPayload.nodeId, statusPayload.originalDiscriminator)
        if (statusPayload.success && statusPayload.nodeId) {
          console.log('It Was a sucesss')
          const deviceToUpdate = discoveredDevices.value.find(
            (d) =>
              d.discriminator === statusPayload.originalDiscriminator ||
              d.discriminator === statusPayload.discriminatorAssociatedWithRequest,
          )
          console.log('DeviceToUpdate', deviceToUpdate)
          if (deviceToUpdate) {
            deviceToUpdate.nodeId = statusPayload.nodeId
            if (
              selectedDevice.value &&
              selectedDevice.value.discriminator === deviceToUpdate.discriminator
            ) {
              selectedDevice.value.nodeId = statusPayload.nodeId
            }
          } else {
            const newDevice: DiscoveredDevice = {
              id: `device_node_${statusPayload.nodeId}`,
              name: `Device ${statusPayload.nodeId}`, // Default name
              nodeId: statusPayload.nodeId,
              discriminator:
                statusPayload.originalDiscriminator ||
                statusPayload.discriminatorAssociatedWithRequest ||
                'N/A',
            }
            if (!discoveredDevices.value.find((d) => d.nodeId === statusPayload.nodeId)) {
              discoveredDevices.value.push(newDevice)
            }
          }
          const newlyCommissioned = discoveredDevices.value.find(
            (d) => d.nodeId === statusPayload.nodeId,
          )
          if (newlyCommissioned) {
            selectedDevice.value = newlyCommissioned
            commissioningLogs.value.push(
              `Device ${newlyCommissioned.name || `Device ${newlyCommissioned.nodeId}`} (Node ID: ${newlyCommissioned.nodeId}) is now selected for control.`,
            )
          }
        }
        break
      case 'attribute_update':
        const attrPayload = message.payload as AttributeUpdatePayload
        if (attrPayload && attrPayload.nodeId) {
          const nodeIdStr = String(attrPayload.nodeId)
          if (!deviceStatus.value[nodeIdStr]) {
            deviceStatus.value[nodeIdStr] = {}
          }
          const key = `${attrPayload.cluster}_${attrPayload.attribute}`
          deviceStatus.value[nodeIdStr][key] = attrPayload.value
          deviceStatus.value = { ...deviceStatus.value }
          console.log(deviceStatus.value[nodeIdStr], 'MEGA LKOG')
        }
        break
      case 'command_response':
        const cmdRespPayload = message.payload as CommandResponsePayload
        commissioningLogs.value.push(
          `[Command Response for Node ${cmdRespPayload.nodeId || 'Unknown'}]: Success: ${cmdRespPayload.success}. Details: ${cmdRespPayload.details || cmdRespPayload.error || ''}`,
        )
        break
      case 'internal_log': // These use 'data' field as per TypedServerToClientMessage
      case 'internal_error':
        commissioningLogs.value.push(`[WebSocket]: ${message.data}`)
        break
      case 'error':
        commissioningLogs.value.push(
          `[Backend Error]: ${message.payload?.message || JSON.stringify(message.payload)}`,
        )
        break
      default:
        const unknownMsg = message as any // Fallback for truly unhandled
        console.warn('Unhandled message type from backend:', unknownMsg.type, unknownMsg)
        commissioningLogs.value.push(
          `[Backend Unhandled]: ${unknownMsg.type} - ${JSON.stringify(unknownMsg.payload || unknownMsg.data)}`,
        )
    }
  }

  function selectDeviceForControl(device: DiscoveredDevice): void {
    if (device && device.nodeId) {
      selectedDevice.value = device
      commissioningLogs.value.push(
        `Selected ${device.name || `Device ${device.nodeId}`} (Node ID: ${device.nodeId}) for control.`,
      )
    } else {
      const name = device.name || `Discriminator ${device.discriminator}`
      commissioningLogs.value.push(
        `Cannot select device: ${name}. It has no Node ID. Please commission it first.`,
      )
      alert(`Device ${name} cannot be controlled as it's not commissioned or Node ID is missing.`)
    }
  }

  return {
    currentStep,
    totalSteps,
    rpiIpAddress,
    hubSetupCommands,
    hubSetupLogs,
    discoveredDevices,
    selectedDevice,
    commissioningLogs,
    deviceStatus,
    progress,
    isHubSetupComplete,
    nextStep,
    previousStep,
    setRpiIp,
    confirmCommand,
    startDeviceDiscovery,
    commissionDevice,
    sendDeviceCommand,
    handleBackendMessage,
    selectDeviceForControl,
  }
})
