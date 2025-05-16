# Matter Framework Wizard - Backend (Golang)

This directory contains the Golang backend application for the Matter Framework Wizard. It handles WebSocket communication with the Vue3 frontend and interacts with `chip-tool` to manage Matter devices.

## Prerequisites

- Go (version 1.20 or later recommended)
- `chip-tool` installed and accessible in your system's PATH, or the `chipToolPath` constant in `handlers.go` updated to its full path.
  - On a Raspberry Pi, `chip-tool` might be installed via `snap` (e.g., `matter-pi-tool.chip-tool`) or built from the Matter SDK source. **Verify the `chipToolPath` constant in `handlers.go`!**
- Relevant permissions for `chip-tool` to access Bluetooth/BLE and network interfaces (often requires `sudo` or specific group memberships).

## Setup

1.  **Clone/Copy Files:** Ensure all `.go` files (`main.go`, `hub.go`, `handlers.go`, `models.go`) and `go.mod` are in this directory.
2.  **Initialize Go Modules (if not already done):**
    ```bash
    go mod init matter-framework-backend
    ```
    (Use the module name specified in your `go.mod` file.)
3.  **Tidy Dependencies:** This will download the required packages (Gin, Gorilla WebSocket, etc.) and update/create `go.sum`.
    ```bash
    go mod tidy
    ```

## Configuration

- **`chipToolPath` in `handlers.go`**: This is CRITICAL. Update this constant to the correct command or path for `chip-tool` on your Raspberry Pi.
  - Examples: `"chip-tool"`, `"/snap/bin/chip-tool"`, `"/home/pi/connectedhomeip/out/chip-tool-arm64/chip-tool"`.
- **`paaTrustStorePath` in `handlers.go`**: If you are working with production-certified Matter devices, you might need to set this path to your PAA root certificates. For testing with development devices, it can often be left commented out or empty.
- **CORS Configuration in `main.go`**: The CORS settings are configured to allow requests from `http://localhost:5173` (default Vite dev server). Adjust if your frontend is served from a different origin.

## Running the Backend

1.  **Navigate to the `backend-golang` directory.**
2.  **Run the application:**

    ```bash
    go run .
    ```

    Or, build an executable first:

    ```bash
    go build -o matter_backend
    ./matter_backend
    ```

    You might need to run the backend with `sudo` if `chip-tool` requires root privileges for its operations (e.g., BLE scanning, network interface access):

    ```bash
    sudo go run .
    ```

    or

    ```bash
    sudo ./matter_backend
    ```

    **Note:** Running the entire Go application as root is generally not recommended for security reasons. A better long-term solution would be to grant specific capabilities to the `chip-tool` executable or run it via `sudo` only for specific commands if absolutely necessary, or use a user in the correct groups (e.g., `bluetooth`, `dialout`).

3.  The server will start, typically on port `:8080`. You should see log output in your terminal.

## Interacting with the Frontend

Once the backend is running:

1.  Start your Vue3 frontend development server (usually `npm run dev` in the `frontend-vue3` directory).
2.  Open the frontend in your browser (e.g., `http://localhost:5173`).
3.  In the frontend wizard (Step 1), enter the IP address of the machine running this Golang backend (e.g., your Raspberry Pi's IP address) and the port (e.g., `192.168.1.XX:8080` - the frontend will construct `ws://<IP>:8080/ws`).
4.  Attempt to connect. Check the terminal output of both the frontend and backend for logs and errors.

## Key Functionality

- **WebSocket Server:** Listens on `/ws` for WebSocket connections from the frontend.
- **Client Management:** Uses a `Hub` to manage active WebSocket clients.
- **Message Handling:**
  - `discover_devices`: Executes `chip-tool discover commissionables` and parses the output.
  - `commission_device`: Executes `chip-tool pairing ble-discriminator` (or similar) to commission a device.
  - `device_command`: Executes `chip-tool <cluster> <command>` to control devices.
- **`chip-tool` Execution:** Uses `os/exec` to run `chip-tool` commands.
- **Output Parsing:** Includes basic parsing for `chip-tool` output. This is often the most fragile part and may need significant refinement based on the exact `chip-tool` version and output format.

## Important Notes & Troubleshooting

- **`chip-tool` Path & Permissions:** This is the most common point of failure. Double-check the path and ensure `chip-tool` can be executed by the user running the Go program, with necessary permissions for BLE/network.
- **`chip-tool` Output Parsing:** The parsing logic in `handlers.go` (e.g., `parseDiscoveryOutput`, parsing commissioning results) is based on common `chip-tool` output patterns but might need adjustments. Verbose logging in `chip-tool` or changes in its output format can break parsing.
- **CORS:** If the frontend cannot connect, check browser console logs for CORS errors. Ensure the `AllowOrigins` in `main.go` matches your frontend's origin.
- **Logging:** The backend includes `log.Printf` statements. Check the terminal output for detailed information and errors.
- **Firewall:** Ensure the port the backend is running on (e.g., 8080) is not blocked by a firewall on the machine running the backend.
- **Device State:** This backend is largely stateless regarding Matter devices themselves, relying on `chip-tool` for interactions. For a production system, you might want to persist information about commissioned devices.
