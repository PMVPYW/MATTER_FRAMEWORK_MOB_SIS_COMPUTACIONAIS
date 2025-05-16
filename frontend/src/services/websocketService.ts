import { useWizardStore } from '@/stores/wizardStore' // Using alias @
import type {
  ClientToServerMessage,
  ServerToClientMessage,
  TypedServerToClientMessage,
} from '@/types' // Using alias @

let socket: WebSocket | null = null
let reconnectAttempts = 0
const MAX_RECONNECT_ATTEMPTS = 5
const RECONNECT_DELAY = 5000 // 5 seconds
let currentWsUrl = '' // Store the URL for reconnection

export function connectWebSocket(url: string): void {
  currentWsUrl = url // Store for potential reconnections
  if (
    socket &&
    (socket.readyState === WebSocket.OPEN || socket.readyState === WebSocket.CONNECTING)
  ) {
    console.log('WebSocket already connected or connecting.')
    if (socket.readyState === WebSocket.OPEN) {
      const wizardStore = useWizardStore()
      wizardStore.handleBackendMessage({
        type: 'internal_log',
        data: 'WebSocket already connected.',
      } as TypedServerToClientMessage)
    }
    return
  }

  socket = new WebSocket(url)
  const wizardStore = useWizardStore()

  wizardStore.handleBackendMessage({
    type: 'internal_log',
    data: `Attempting to connect to WebSocket: ${url}`,
  } as TypedServerToClientMessage)

  socket.onopen = () => {
    console.log('WebSocket connection established')
    wizardStore.handleBackendMessage({
      type: 'internal_log',
      data: 'WebSocket Connection Established Successfully!',
    } as TypedServerToClientMessage)
    reconnectAttempts = 0
  }

  socket.onmessage = (event: MessageEvent) => {
    try {
      const message: TypedServerToClientMessage = JSON.parse(event.data as string)
      wizardStore.handleBackendMessage(message)
    } catch (error: any) {
      console.error('Error parsing WebSocket message:', error, 'Raw data:', event.data)
      wizardStore.handleBackendMessage({
        type: 'internal_error',
        data: `Error parsing message from backend: ${error?.message || 'Unknown parse error'}. Raw: ${event.data}`,
      } as TypedServerToClientMessage)
    }
  }

  socket.onerror = (event: Event) => {
    // event is generic, error details are limited
    console.error('WebSocket error event:', event)
    let errorMessage = 'WebSocket connection error occurred.'
    // For more detailed errors, you'd typically see them logged by the browser or in onclose.

    wizardStore.handleBackendMessage({
      type: 'internal_error',
      data: errorMessage,
    } as TypedServerToClientMessage)
  }

  socket.onclose = (event: CloseEvent) => {
    console.log('WebSocket connection closed:', event)
    let reason = `Code: ${event.code}, Reason: ${event.reason || 'No reason specified.'}`
    if (!event.wasClean) {
      reason += ' (Connection was not closed cleanly)'
    }
    wizardStore.handleBackendMessage({
      type: 'internal_log',
      data: `WebSocket Closed. ${reason}`,
    } as TypedServerToClientMessage)
    socket = null

    if (reconnectAttempts < MAX_RECONNECT_ATTEMPTS && currentWsUrl) {
      reconnectAttempts++
      wizardStore.handleBackendMessage({
        type: 'internal_log',
        data: `Attempting to reconnect (${reconnectAttempts}/${MAX_RECONNECT_ATTEMPTS})...`,
      } as TypedServerToClientMessage)
      setTimeout(() => {
        connectWebSocket(currentWsUrl)
      }, RECONNECT_DELAY)
    } else if (currentWsUrl) {
      // Only if a URL was set (i.e., not a manual disconnect without URL)
      wizardStore.handleBackendMessage({
        type: 'internal_error',
        data: `Failed to reconnect after ${MAX_RECONNECT_ATTEMPTS} attempts.`,
      } as TypedServerToClientMessage)
      reconnectAttempts = 0
    }
  }
}

export function sendMessage(message: ClientToServerMessage): void {
  if (socket && socket.readyState === WebSocket.OPEN) {
    try {
      const messageString = JSON.stringify(message)
      socket.send(messageString)
      console.log('Message sent:', message)
    } catch (error: any) {
      console.error('Error sending message (JSON stringify failed):', error)
      const wizardStore = useWizardStore()
      wizardStore.handleBackendMessage({
        type: 'internal_error',
        data: 'Failed to prepare message for sending (JSON error).',
      } as TypedServerToClientMessage)
    }
  } else {
    console.error('WebSocket is not connected. Cannot send message:', message)
    const wizardStore = useWizardStore()
    wizardStore.handleBackendMessage({
      type: 'internal_error',
      data: 'Cannot send message: WebSocket not connected.',
    } as TypedServerToClientMessage)
  }
}

export function disconnectWebSocket(): void {
  if (socket) {
    console.log('Manually disconnecting WebSocket.')
    currentWsUrl = '' // Clear current URL to prevent auto-reconnection by onclose
    reconnectAttempts = MAX_RECONNECT_ATTEMPTS // Prevent auto-reconnection
    socket.close(1000, 'User initiated disconnect')
    socket = null
    const wizardStore = useWizardStore()
    wizardStore.handleBackendMessage({
      type: 'internal_log',
      data: 'WebSocket manually disconnected.',
    } as TypedServerToClientMessage)
  }
}
