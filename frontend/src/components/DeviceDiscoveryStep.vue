<template>
  <div class="step-container device-discovery-step">
    <h3>Step 2: Discover & Commission Matter Devices</h3>
    <p>
      Ensure your Matter devices are in pairing/commissioning mode. The backend will use `chip-tool`
      to scan for commissionable devices.
    </p>
    <button @click="startDiscovery" :disabled="isDiscovering">
      {{ isDiscovering ? 'Discovering...' : 'Search for New Devices' }}
    </button>

    <div v-if="wizardStore.discoveredDevices.length > 0" class="discovered-devices-list">
      <h4>Discovered Devices:</h4>
      <ul>
        <li
          v-for="device in wizardStore.discoveredDevices"
          :key="device.id || device.discriminator"
        >
          <div class="device-info">
            <strong>{{
              device.name || `Device VID:${device.vendorId}/PID:${device.productId}`
            }}</strong
            ><br />
            <small
              >Discriminator: {{ device.discriminator }} | VID: {{ device.vendorId }} | PID:
              {{ device.productId }}</small
            ><br />
            <small v-if="device.nodeId" class="commissioned-badge"
              >Commissioned (Node ID: {{ device.nodeId }})</small
            >
            <small v-else class="not-commissioned-badge">Not Commissioned</small>
          </div>
          <button
            @click="wizardStore.commissionDevice(device)"
            :disabled="!!device.nodeId || isDeviceCommissioning(device)"
            v-if="!device.nodeId"
          >
            {{ isDeviceCommissioning(device) ? 'Commissioning...' : 'Commission Device' }}
          </button>
          <button
            @click="wizardStore.selectDeviceForControl(device)"
            v-if="device.nodeId"
            class="control-btn"
          >
            Control This Device
          </button>
        </li>
      </ul>
    </div>
    <p v-else-if="!isDiscovering && searchAttempted">
      No devices discovered. Ensure devices are in pairing mode and Bluetooth is active on the
      Raspberry Pi. Check logs for details.
    </p>
    <p v-else-if="!isDiscovering && !searchAttempted">Click "Search for New Devices" to begin.</p>
  </div>
</template>

<script setup lang="ts">
import { ref, Ref } from 'vue'
import { useWizardStore } from '@/stores/wizardStore' // Using alias
import type { DiscoveredDevice } from '@/types' // Using alias

const wizardStore = useWizardStore()
const isDiscovering: Ref<boolean> = ref(false)
const searchAttempted: Ref<boolean> = ref(false)

async function startDiscovery(): Promise<void> {
  isDiscovering.value = true
  searchAttempted.value = true
  wizardStore.startDeviceDiscovery()
  setTimeout(() => {
    isDiscovering.value = false
  }, 35000) // Reset button state after a while
}

const isDeviceCommissioning = (device: DiscoveredDevice): boolean => {
  const logs = wizardStore.commissioningLogs
  // A verificação de início continua a mesma
  const commissionStartLog = logs.some((log) =>
    log.includes(`request for discriminator ${device.discriminator}`),
  )

  // A verificação de fim agora é muito mais fiável
  // Procura pela nossa nova mensagem de log estruturada
  const commissionEndLog = logs.some(
    (log) =>
      log.includes('[Commissioning Status]:') &&
      log.includes(`Request for Discriminator: ${device.discriminator}`),
  )

  return commissionStartLog && !commissionEndLog
}
</script>

<style scoped>
/* Styles are primarily in main.css */
.device-discovery-step h3 {
  margin-bottom: 15px;
}
.discovered-devices-list {
  margin-top: 20px;
}
.device-info {
  flex-grow: 1;
}
.device-info strong {
  font-size: 1.05em;
}
.device-info small {
  display: block;
  color: #666;
  font-size: 0.85em;
}
.commissioned-badge {
  color: green;
  font-weight: bold;
}
.not-commissioned-badge {
  color: #c58800;
}
.control-btn {
  background-color: #17a2b8;
}
.control-btn:hover {
  background-color: #138496;
}
</style>
