import {
    BoxRenderable,
    InputRenderable,
    SelectRenderable,
    TextRenderable,
    createCliRenderer,
    type CliRenderer,
    type KeyEvent,
} from "@opentui/core"
import { join } from "node:path"
import { Engine, type EngineEvent, type Mode } from "./engine"

// Must match backend.ProfileNames in backend/audio.go (index == backend.Profile).
const PROFILES = ["Low latency (10 ms)", "Balanced (20 ms)", "Battery saver (40 ms)"]
const MODES: { id: Mode; label: string }[] = [
    { id: "usb", label: "USB / ADB" },
{ id: "wireless", label: "Wireless ADB" },
{ id: "direct", label: "WiFi Direct (No ADB)" },
]

type Field = "mode" | "wireless" | "ip" | "devices" | "port" | "profile" | "silence"

const C = {
    title: "#ff87d7",
    accent: "#ff87d7",
    dim: "#626262",
    text: "#e4e4e4",
    ok: "#5fd75f",
    err: "#ff5f5f",
    border: "#585858",
}

async function resolveEngine(): Promise<string> {
    const candidates = [
        process.env.SONIDEX_ENGINE,
        join(import.meta.dir, "..", "..", "sonidex-engine"), // repo checkout: ./sonidex-engine
        join(import.meta.dir, "..", "sonidex-engine"),
        join(import.meta.dir, "sonidex-engine"),
        Bun.which("sonidex-engine"),
    ].filter((p): p is string => !!p)
    for (const p of candidates) if (await Bun.file(p).exists()) return p
        console.error(
            "sonidex-engine not found. Build it with:\n  go build -o sonidex-engine ./cmd/sonidex-engine\n" +
            "or point SONIDEX_ENGINE at the binary.",
        )
        process.exit(1)
}

async function main() {
    const enginePath = await resolveEngine()

    const renderer: CliRenderer = await createCliRenderer({
        exitOnCtrlC: false, // we shut the engine down ourselves first
        targetFps: 15, // static UI: low redraw rate keeps CPU wakeups down
    })

    // ---- state ----------------------------------------------------------
    let modeIdx = 0
    let profile = 0
    let silence = process.env.SONIDEX_SILENCE_SUPPRESS !== "0"
    let running = false
    let devices: string[] = []
    let focus: Field = "devices"
    let lastErr = ""
    const logs: string[] = []

    const mode = () => MODES[modeIdx]!.id

    // ---- widgets --------------------------------------------------------
    const root = new BoxRenderable(renderer, {
        id: "root",
        flexDirection: "column",
        width: "100%",
        height: "100%",
        padding: 1,
        gap: 1,
    })
    renderer.root.add(root)

    const title = new TextRenderable(renderer, { id: "title", content: "Sonidex Streamer (TUI)", fg: C.title })
    const modeRow = new TextRenderable(renderer, { id: "mode", content: "", fg: C.text })

    const wirelessBox = new BoxRenderable(renderer, {
        id: "wireless-box",
        flexDirection: "row",
        gap: 1,
        height: 3,
        border: true,
        borderColor: C.border,
        title: "Device ip:port (Enter to connect)",
    })
    const wirelessInput = new InputRenderable(renderer, {
        id: "wireless-input",
        width: 30,
        placeholder: "192.168.1.42:5555",
        placeholderColor: C.dim,
        textColor: C.text,
        cursorColor: C.accent,
        maxLength: 64,
    })
    wirelessBox.add(wirelessInput)

    const ipBox = new BoxRenderable(renderer, {
        id: "ip-box",
        height: 3,
        border: true,
        borderColor: C.border,
        title: "Phone IP",
    })
    const ipInput = new InputRenderable(renderer, {
        id: "ip-input",
        width: 30,
        placeholder: "192.168.1.42",
        placeholderColor: C.dim,
        textColor: C.text,
        cursorColor: C.accent,
        maxLength: 64,
    })
    ipBox.add(ipInput)

    const deviceBox = new BoxRenderable(renderer, {
        id: "device-box",
        height: 8,
        border: true,
        borderColor: C.border,
        title: "Android Target Device",
    })
    const deviceSelect = new SelectRenderable(renderer, {
        id: "device-select",
        width: "100%",
        height: 6,
        options: [],
        showDescription: false,
        backgroundColor: "transparent",
        textColor: C.text,
        selectedBackgroundColor: "transparent",
        selectedTextColor: C.accent,
        focusedBackgroundColor: "transparent",
        focusedTextColor: C.text,
    })
    deviceBox.add(deviceSelect)

    const portBox = new BoxRenderable(renderer, {
        id: "port-box",
        height: 3,
        border: true,
        borderColor: C.border,
        title: "Port",
    })
    const portInput = new InputRenderable(renderer, {
        id: "port-input",
        width: 10,
        value: "8080",
        placeholder: "8080",
        placeholderColor: C.dim,
        textColor: C.text,
        cursorColor: C.accent,
        maxLength: 5,
    })
    portBox.add(portInput)

    const profileRow = new TextRenderable(renderer, { id: "profile", content: "", fg: C.text })
    const silenceRow = new TextRenderable(renderer, { id: "silence", content: "", fg: C.text })
    const statusRow = new TextRenderable(renderer, { id: "status", content: "Ready", fg: C.dim })

    const logBox = new BoxRenderable(renderer, {
        id: "log-box",
        height: 10,
        border: true,
        borderColor: C.border,
        title: "Log",
    })
    const logText = new TextRenderable(renderer, { id: "log", content: "", fg: C.dim })
    logBox.add(logText)

    const help = new TextRenderable(renderer, { id: "help", content: "", fg: C.dim })

    root.add(title)
    root.add(modeRow)
    root.add(wirelessBox)
    root.add(ipBox)
    root.add(deviceBox)
    root.add(portBox)
    root.add(profileRow)
    root.add(silenceRow)
    root.add(statusRow)
    root.add(logBox)
    root.add(help)

    // ---- helpers --------------------------------------------------------
    const order = (): Field[] => {
        switch (mode()) {
            case "wireless":
                return ["mode", "wireless", "devices", "port", "profile", "silence"]
            case "direct":
                return ["mode", "ip", "port", "profile", "silence"]
            default:
                return ["mode", "devices", "port", "profile", "silence"]
        }
    }

    const isTextField = (f: Field) => f === "wireless" || f === "ip" || f === "port"

    const pushLog = (s: string) => {
        logs.push(s)
        if (logs.length > 8) logs.splice(0, logs.length - 8)
    }

    const row = (label: string, value: string, f: Field) => {
        const marker = focus === f ? "> " : "  "
        return `${marker}${label}: ${value}`
    }

    function refresh() {
        // visibility depends on mode
        wirelessBox.visible = mode() === "wireless"
        ipBox.visible = mode() === "direct"
        deviceBox.visible = mode() !== "direct"

        modeRow.content = row("Mode (m / ←→)", MODES[modeIdx]!.label, "mode")
        modeRow.fg = focus === "mode" ? C.accent : C.text
        profileRow.content = row("Profile (l / ←→)", PROFILES[profile]!, "profile")
        profileRow.fg = focus === "profile" ? C.accent : C.text
        silenceRow.content = row("Pause on silence (s / space)", silence ? "on" : "off", "silence")
        silenceRow.fg = focus === "silence" ? C.accent : C.text

        for (const [box, f] of [
            [wirelessBox, "wireless"],
             [ipBox, "ip"],
             [deviceBox, "devices"],
             [portBox, "port"],
        ] as [BoxRenderable, Field][]) {
            box.borderColor = focus === f ? C.accent : C.border
        }

        if (lastErr) {
            statusRow.content = lastErr
            statusRow.fg = C.err
        } else if (running) {
            statusRow.content = "Streaming active..."
            statusRow.fg = C.ok
        } else {
            statusRow.content = "Ready"
            statusRow.fg = C.dim
        }

        logText.content = logs.join("\n")
        help.content =
        "↑/↓ select · r refresh · tab next field · m mode · l profile · s silence · " +
        (running ? "enter: stop streaming" : "enter: start streaming") +
        " · q quit"

        renderer.requestRender()
    }

    function applyFocus() {
        wirelessInput.blur()
        ipInput.blur()
        portInput.blur()
        deviceSelect.blur()
        if (focus === "wireless") wirelessInput.focus()
            else if (focus === "ip") ipInput.focus()
                else if (focus === "port") portInput.focus()
                    else if (focus === "devices") deviceSelect.focus()
                        refresh()
    }

    function moveFocus(delta: number) {
        const o = order()
        const i = Math.max(0, o.indexOf(focus))
        focus = o[(i + delta + o.length) % o.length]!
        applyFocus()
    }

    function setDevices(list: string[]) {
        devices = list
        deviceSelect.options = list.map((d) => ({ name: d, description: "", value: d }))
        if (list.length > 0) deviceSelect.setSelectedIndex(0)
    }

    // ---- engine ---------------------------------------------------------
    let quitting = false
    const quit = async () => {
        if (quitting) return
            quitting = true
            await engine.quit()
            renderer.destroy()
            process.exit(0)
    }

    const onEvent = (e: EngineEvent) => {
        switch (e.ev) {
            case "devices":
                if (e.error) {
                    lastErr = e.error
                    setDevices([])
                } else {
                    lastErr = ""
                    setDevices(e.devices)
                }
                break
            case "wireless":
                if (!e.ok) {
                    lastErr = "adb connect failed: " + (e.error ?? "unknown error")
                } else {
                    lastErr = ""
                    pushLog("connected — refreshing devices")
                    engine.listDevices()
                }
                break
            case "status":
                if (e.err) lastErr = e.text
                    pushLog(e.text)
                    break
            case "started":
                running = true
                lastErr = ""
                pushLog(`streaming to ${e.label} on port ${e.port}`)
                break
            case "stopped":
                running = false
                if (e.error) {
                    lastErr = e.error
                    pushLog(e.error)
                } else {
                    pushLog("stream stopped")
                }
                break
        }
        refresh()
    }

    const engine = new Engine(enginePath, onEvent, (code) => {
        if (quitting) return
            lastErr = `engine exited unexpectedly (code ${code})`
            running = false
            refresh()
    })

    // ---- actions --------------------------------------------------------
    function toggleStream() {
        if (running) {
            engine.stop()
            return
        }
        const port = portInput.value.trim() || "8080"
        if (mode() === "direct") {
            if (!ipInput.value.trim()) {
                lastErr = "enter the phone's IP address"
                refresh()
                return
            }
        } else if (devices.length === 0) {
            lastErr = "no ADB devices — press r to refresh"
            refresh()
            return
        }
        lastErr = ""
        engine.start({
            mode: mode(),
                     serial: deviceSelect.getSelectedOption()?.value as string | undefined,
                     ip: ipInput.value.trim(),
                     port,
                     profile,
                     silence,
        })
    }

    function connectWireless() {
        const addr = wirelessInput.value.trim()
        if (!addr) {
            lastErr = "enter the device's ip:port first"
            refresh()
            return
        }
        engine.connectWireless(addr)
    }

    const cycleMode = (d: number) => {
        if (running) return
            modeIdx = (modeIdx + d + MODES.length) % MODES.length
            lastErr = ""
            focus = "devices"
            if (mode() === "direct") focus = "ip"
                else if (mode() === "wireless") focus = "wireless"
                    applyFocus()
    }
    const cycleProfile = (d: number) => {
        if (running) return
            profile = (profile + d + PROFILES.length) % PROFILES.length
            refresh()
    }
    const toggleSilence = () => {
        if (running) return
            silence = !silence
            refresh()
    }

    // ---- keys -----------------------------------------------------------
    renderer.keyInput.on("keypress", (key: KeyEvent) => {
        if (key.ctrl && key.name === "c") return void quit()

            if (key.name === "tab") return moveFocus(key.shift ? -1 : 1)

                if (key.name === "return" || key.name === "enter") {
                    if (focus === "wireless" && !running) return connectWireless()
                        return toggleStream()
                }

                if (key.name === "escape" && isTextField(focus)) {
                    focus = "mode"
                    return applyFocus()
                }

                // Row-specific arrows (selects/inputs consume their own keys).
                if (focus === "mode" && (key.name === "left" || key.name === "right"))
                    return cycleMode(key.name === "right" ? 1 : -1)
                    if (focus === "profile" && (key.name === "left" || key.name === "right"))
                        return cycleProfile(key.name === "right" ? 1 : -1)
                        if (focus === "silence" && (key.name === "left" || key.name === "right" || key.name === "space"))
                            return toggleSilence()
                            if (focus === "mode" && key.name === "space") return cycleMode(1)
                                if (focus === "profile" && key.name === "space") return cycleProfile(1)
                                    if (focus === "mode" || focus === "profile" || focus === "silence") {
                                        if (key.name === "up") return moveFocus(-1)
                                            if (key.name === "down") return moveFocus(1)
                                    }

                                    // Letter hotkeys must not steal characters while typing in a text field.
                                    if (isTextField(focus) || key.ctrl || key.meta) return
                                        switch (key.name) {
                                            case "q":
                                                return void quit()
                                            case "m":
                                                return cycleMode(1)
                                            case "l":
                                                return cycleProfile(1)
                                            case "s":
                                                return toggleSilence()
                                            case "r":
                                                if (!running && mode() !== "direct") engine.listDevices()
                                                    return
                                            case "c":
                                                if (mode() === "wireless") return connectWireless()
                                        }
    })

    process.on("SIGTERM", () => void quit())
    process.on("SIGHUP", () => void quit())

    applyFocus()
    engine.listDevices()
}

main()
