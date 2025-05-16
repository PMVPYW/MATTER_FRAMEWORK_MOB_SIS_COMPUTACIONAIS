<template>
  <div class="step-container hub-setup-step">
    <h3>Step 1: Raspberry Pi Hub Setup Instructions</h3>
    <p>
      Follow these instructions to set up your Raspberry Pi as a Matter Hub. Ensure your Raspberry
      Pi has an OS (e.g., Raspberry Pi OS Lite 64-bit) installed, is connected to your network, and
      you can SSH into it or run commands directly.
    </p>
    <p>
      <strong>Note:</strong> These commands are illustrative. You may need to adapt them based on
      your specific RPi setup, Matter SDK version, and chosen components (like OTBR). Execute these
      commands on your Raspberry Pi's terminal.
    </p>

    <div v-if="wizardStore.hubSetupCommands && wizardStore.hubSetupCommands.length > 0">
      <div v-for="cmd in wizardStore.hubSetupCommands" :key="cmd.id" class="command-block">
        <h4>{{ cmd.id }}. {{ cmd.instruction }}</h4>
        <pre><code>{{ cmd.command }}</code></pre>
        <button @click="copyCommand(cmd.command)" class="copy-btn">Copy Command</button>
        <p class="expected-output">
          <strong>Expected Outcome (General Idea):</strong> {{ cmd.expectedOutput }}
        </p>
        <button
          @click="wizardStore.confirmCommand(cmd.id)"
          :disabled="cmd.confirmed"
          :class="{ 'confirmed-btn': cmd.confirmed, 'confirm-btn': !cmd.confirmed }"
        >
          {{ cmd.confirmed ? '✔️ Marked as Completed' : 'Mark as Completed on RPi' }}
        </button>
      </div>
    </div>
    <p v-else>No setup commands loaded. This might be an issue with the wizard configuration.</p>

    <div v-if="wizardStore.isHubSetupComplete" class="completion-message">
      <p>🎉 All Raspberry Pi hub setup steps have been marked as completed!</p>
      <p>You can now proceed to the next step to discover and commission Matter devices.</p>
    </div>
    <div v-else class="completion-message">
      <p>
        Please ensure all commands are executed on your Raspberry Pi and then marked as completed
        here to proceed.
      </p>
    </div>
  </div>
</template>

<script setup lang="ts">
import { useWizardStore } from '@/stores/wizardStore' // Using alias

const wizardStore = useWizardStore()

async function copyCommand(commandText: string): Promise<void> {
  try {
    await navigator.clipboard.writeText(commandText)
    alert('Command copied to clipboard!')
  } catch (err) {
    console.error('Failed to copy command: ', err)
    alert('Failed to copy command. Your browser might not support this feature or requires HTTPS.')
  }
}
</script>

<style scoped>
/* Styles are primarily in main.css */
.hub-setup-step h3 {
  margin-bottom: 15px;
}
.copy-btn {
  background-color: #007bff;
}
.copy-btn:hover {
  background-color: #0056b3;
}
.confirm-btn {
  background-color: #ffc107;
  color: #333;
}
.confirm-btn:hover {
  background-color: #e0a800;
}
.confirmed-btn {
  background-color: #28a745;
  color: white;
  cursor: default;
}
.expected-output {
  font-size: 0.9em;
  color: #555;
  margin-top: 8px;
  background-color: #f0f0f0;
  padding: 5px;
  border-radius: 3px;
}
.completion-message {
  margin-top: 20px;
  padding: 15px;
  border-radius: 5px;
  background-color: #e6ffed;
  border: 1px solid #b2dfc1;
  color: #2d6a4f;
}
.completion-message p {
  margin: 5px 0;
}
</style>
