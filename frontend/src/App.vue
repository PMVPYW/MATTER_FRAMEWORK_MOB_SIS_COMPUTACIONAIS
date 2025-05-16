<template>
  <div id="app-container">
    <h1>Matter Onboarding & Control Wizard (TS)</h1>

    <div class="progress-bar">
      <div class="progress" :style="{ width: wizardStore.progress + '%' }">
        {{ Math.round(wizardStore.progress) }}%
      </div>
    </div>

    <p class="step-indicator">
      Step {{ wizardStore.currentStep }} of {{ wizardStore.totalSteps }}: {{ currentStepTitle }}
    </p>

    <div v-if="wizardStore.currentStep === 1 && !rpiIpAddressConnected" class="rpi-connect-section">
      <h2>Connect to Raspberry Pi Backend</h2>
      <label for="rpi-ip">Enter Raspberry Pi IP Address (e.g., 192.168.1.XX):</label>
      <input type="text" id="rpi-ip" v-model="rpiIpInput" placeholder="Enter RPi IP Address" />
      <button @click="connectToBackend" :disabled="!rpiIpInput">Connect to RPi</button>
      <p
        v-if="connectionMessage"
        :class="{
          'connection-success': wsConnected,
          'connection-error': !wsConnected && !!connectionMessage,
        }"
      >
        {{ connectionMessage }}
      </p>
    </div>

    <div class="content-area">
      <div v-if="wizardStore.currentStep === 1">
        <HubSetupStep v-if="rpiIpAddressConnected" />
        <p v-else-if="!rpiIpAddressConnected && rpiIpInput">
          Please connect to the RPi backend to proceed with hub setup.
        </p>
      </div>
      <div v-else-if="wizardStore.currentStep === 2">
        <DeviceDiscoveryStep />
      </div>
      <div v-else-if="wizardStore.currentStep === 3">
        <DeviceControl />
      </div>
    </div>

    <div class="navigation-buttons">
      <button @click="wizardStore.previousStep()" :disabled="wizardStore.currentStep === 1">
        Previous
      </button>
      <button @click="wizardStore.nextStep()" :disabled="!canProceedToNextStep">Next</button>
    </div>

    <div class="logs-container">
      <h3>Activity Logs:</h3>
      <pre class="logs-output">{{ combinedLogs }}</pre>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, watch, Ref, ComputedRef } from 'vue'
import { useWizardStore } from '@/stores/wizardStore' // Using alias
import { connectWebSocket, disconnectWebSocket } from '@/services/websocketService' // Using alias
import HubSetupStep from '@/components/HubSetupStep.vue'
import DeviceDiscoveryStep from '@/components/DeviceDiscoveryStep.vue'
import DeviceControl from '@/components/DeviceControl.vue'

const wizardStore = useWizardStore()
const rpiIpInput: Ref<string> = ref(wizardStore.rpiIpAddress || '')
const rpiIpAddressConnected: Ref<boolean> = ref(!!wizardStore.rpiIpAddress)
const wsConnected: Ref<boolean> = ref(false)
const connectionMessage: Ref<string> = ref('')

function connectToBackend(): void {
  if (!rpiIpInput.value) {
    connectionMessage.value = 'Please enter the Raspberry Pi IP address.'
    wsConnected.value = false
    return
  }
  const ip = rpiIpInput.value.trim()
  wizardStore.setRpiIp(ip)

  connectionMessage.value = `Attempting to connect to ws://${ip}:8080/ws...`
  wsConnected.value = false

  disconnectWebSocket()
  connectWebSocket(`ws://${ip}:8080/ws`)
  rpiIpAddressConnected.value = true

  setTimeout(() => {
    const logs = wizardStore.commissioningLogs.concat(wizardStore.hubSetupLogs)
    if (logs.some((log) => log.includes('WebSocket Connection Established'))) {
      connectionMessage.value = `Successfully connected to RPi at ${ip}!`
      wsConnected.value = true
    } else if (
      !logs.some((log) => log.includes('WebSocket Closed')) &&
      !logs.some((log) => log.includes('Failed to reconnect'))
    ) {
      if (
        logs.some(
          (log) => log.toLowerCase().includes('error') || log.toLowerCase().includes('failed'),
        )
      ) {
        connectionMessage.value = `Failed to connect to RPi at ${ip}. Check IP and backend status. See logs for details.`
        wsConnected.value = false
      }
    }
  }, 2000)
}

const currentStepTitle: ComputedRef<string> = computed(() => {
  switch (wizardStore.currentStep) {
    case 1:
      return 'Raspberry Pi Hub Setup'
    case 2:
      return 'Discover & Commission Devices'
    case 3:
      return 'Control Devices'
    default:
      return 'Unknown Step'
  }
})

const canProceedToNextStep: ComputedRef<boolean> = computed(() => {
  if (wizardStore.currentStep === wizardStore.totalSteps) return false
  if (wizardStore.currentStep === 1) {
    return rpiIpAddressConnected.value && wsConnected.value && wizardStore.isHubSetupComplete
  }
  if (wizardStore.currentStep === 2) {
    return true
  }
  return true
})

const combinedLogs: ComputedRef<string> = computed(() => {
  const hubLogs = Array.isArray(wizardStore.hubSetupLogs) ? wizardStore.hubSetupLogs : []
  const commLogs = Array.isArray(wizardStore.commissioningLogs) ? wizardStore.commissioningLogs : []
  return hubLogs.concat(commLogs).join('\n')
})

watch(
  () => wizardStore.rpiIpAddress,
  (newIp) => {
    if (newIp && !rpiIpInput.value) {
      rpiIpInput.value = newIp
    }
    if (!newIp) {
      rpiIpAddressConnected.value = false
      wsConnected.value = false
      connectionMessage.value = 'RPi IP Address cleared.'
    }
  },
)
</script>

<style scoped>
/* Styles are in main.css or App.vue global style from previous JS version */
#app-container {
  /* Using global styles from main.css now */
}

.step-indicator {
  text-align: center;
  font-size: 1.1em;
  color: #555;
  margin-bottom: 20px;
}

.rpi-connect-section {
  background-color: #f9f9f9;
  padding: 20px;
  border-radius: 6px;
  margin-bottom: 20px;
  border: 1px solid #e0e0e0;
}

.rpi-connect-section h2 {
  margin-top: 0;
  margin-bottom: 15px;
  font-size: 1.3em;
}

.rpi-connect-section input[type='text'] {
  width: calc(100% - 130px); /* Adjust based on button width + margin */
  margin-right: 10px;
  display: inline-block;
  vertical-align: middle; /* Align with button */
}

.rpi-connect-section button {
  padding: 10px 15px; /* Ensure consistent padding with global styles */
  vertical-align: middle; /* Align with input */
}

.connection-success {
  color: green;
  font-weight: bold;
  margin-top: 10px;
}

.connection-error {
  color: red;
  font-weight: bold;
  margin-top: 10px;
}

.content-area {
  margin-top: 20px;
}
</style>
