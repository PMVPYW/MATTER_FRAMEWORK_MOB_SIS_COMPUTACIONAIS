<template>
  <div class="step-container device-control-step">
    <h3>Step 3: Control Matter Devices</h3>
    <div v-if="commissionedDevices.length === 0">
      <p>
        No devices have been successfully commissioned yet, or no commissioned devices are
        available.
      </p>
      <p>Please go back to Step 2 to discover and commission devices.</p>
    </div>

    <div
      v-if="commissionedDevices.length > 0 && !wizardStore.selectedDevice?.nodeId"
      class="device-selection"
    >
      <h4>Select a Commissioned Device to Control:</h4>
      <ul>
        <li v-for="device in commissionedDevices" :key="String(device.nodeId)">
          <span>{{ device.name || `Device ${device.nodeId}` }} (Node ID: {{ device.nodeId }})</span>
          <button @click="wizardStore.selectDeviceForControl(device)">Select</button>
        </li>
      </ul>
    </div>

    <div
      v-if="wizardStore.selectedDevice && wizardStore.selectedDevice.nodeId"
      class="selected-device-controls"
    >
      <h4>
        Controlling:
        <strong>{{
          wizardStore.selectedDevice.name || `Device ${wizardStore.selectedDevice.nodeId}`
        }}</strong>
        (Node ID: {{ wizardStore.selectedDevice.nodeId }})
      </h4>
      <button @click="clearSelectedDevice" class="deselect-button">Change Device</button>

      <div class="control-group onoff-control">
        <h5>On/Off Control (Cluster: OnOff)</h5>
        <button @click="sendCommand('OnOff', 'On')">Turn On</button>
        <button @click="sendCommand('OnOff', 'Off')">Turn Off</button>
        <button @click="sendCommand('OnOff', 'Toggle')">Toggle</button>
        <p>
          Current Status: <strong>{{ getDeviceStatus('OnOff_OnOff', 'Unknown') }}</strong>
        </p>
      </div>

      <div class="control-group level-control">
        <h5>Brightness Control (Cluster: LevelControl)</h5>
        <input
          type="range"
          min="0"
          max="254"
          v-model.number="brightnessLevel"
          @change="setBrightness"
          class="brightness-slider"
          id="brightnessSlider"
        />
        <label for="brightnessSlider" class="brightness-value">{{ brightnessLevel }} (0-254)</label>
        <button @click="setBrightness" class="set-brightness-btn">Set Brightness</button>
        <p>
          Current Brightness:
          <strong>{{ getDeviceStatus('LevelControl_CurrentLevel', 'Unknown') }}</strong>
        </p>
      </div>
    </div>
    <div v-else-if="commissionedDevices.length > 0">
      <p>Please select a commissioned device from the list above to control it.</p>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, watch, computed, Ref, ComputedRef } from 'vue'
import { useWizardStore } from '@/stores/wizardStore' // Using alias
import type { DiscoveredDevice } from '@/types' // Using alias

const wizardStore = useWizardStore()
const brightnessLevel: Ref<number> = ref(128)

const commissionedDevices: ComputedRef<DiscoveredDevice[]> = computed(() => {
  return wizardStore.discoveredDevices.filter((d) => !!d.nodeId)
})

function getDeviceStatus(attributeKey: string, defaultValue: any = 'N/A'): any {
  if (wizardStore.selectedDevice && wizardStore.selectedDevice.nodeId) {
    const nodeIdStr = String(wizardStore.selectedDevice.nodeId)
    const status = wizardStore.deviceStatus[nodeIdStr]
    return status && status[attributeKey] !== undefined ? status[attributeKey] : defaultValue
  }
  return defaultValue
}

watch(
  () => wizardStore.selectedDevice,
  (newDevice) => {
    if (newDevice && newDevice.nodeId) {
      const currentBrightness = getDeviceStatus('LevelControl_CurrentLevel')
      console.log('currentBrightness', currentBrightness)
      if (currentBrightness !== 'Unknown' && currentBrightness !== undefined) {
        brightnessLevel.value = parseInt(String(currentBrightness), 10)
      } else {
        brightnessLevel.value = 128
      }
    }
  },
  { deep: true, immediate: true },
)

watch(
  () => wizardStore.deviceStatus,
  () => {
    if (wizardStore.selectedDevice && wizardStore.selectedDevice.nodeId) {
      const currentBrightness = getDeviceStatus('LevelControl_CurrentLevel')
      if (
        currentBrightness !== 'Unknown' &&
        currentBrightness !== undefined &&
        brightnessLevel.value !== parseInt(String(currentBrightness), 10)
      ) {
        brightnessLevel.value = parseInt(String(currentBrightness), 10)
      }
    }
  },
  { deep: true },
)

function sendCommand(cluster: string, command: string, params: Record<string, any> = {}): void {
  if (wizardStore.selectedDevice && wizardStore.selectedDevice.nodeId) {
    console.log(
      'endpointId: wizardStore.selectedDevice?.endpointId: ',
      wizardStore.selectedDevice.endpointId,
    )

    const finalParams = {
      endpointId: wizardStore.selectedDevice?.endpointId || 13,
      ...params,
    }
    wizardStore.sendDeviceCommand(wizardStore.selectedDevice.nodeId, cluster, command, finalParams)
  } else {
    alert('No device selected or device has no Node ID.')
  }
}

function setBrightness(): void {
  sendCommand('LevelControl', 'MoveToLevel', {
    level: brightnessLevel.value, // Already a number due to v-model.number
    transitionTime: 0,
    optionMask: 0,
    optionOverride: 0,
  })
}

function clearSelectedDevice(): void {
  wizardStore.selectedDevice = null
}
</script>

<style scoped>
/* Styles are primarily in main.css */
.device-control-step h3,
.device-control-step h4 {
  margin-bottom: 15px;
}
.selected-device-controls h4 {
  display: inline-block;
  margin-right: 15px;
}
.deselect-button {
  background-color: #6c757d;
  font-size: 0.9em;
  padding: 6px 10px;
  margin-bottom: 15px;
}
.deselect-button:hover {
  background-color: #545b62;
}
.brightness-slider {
  width: 200px;
  margin-right: 10px;
}
.brightness-value {
  display: inline-block;
  width: 70px;
  font-weight: bold;
}
.set-brightness-btn {
  background-color: #007bff;
}
.set-brightness-btn:hover {
  background-color: #0056b3;
}
.device-selection ul {
  margin-top: 10px;
}
</style>
