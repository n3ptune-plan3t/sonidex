import { spawn, type Subprocess } from "bun"

export type Mode = "usb" | "wireless" | "direct"

export type EngineEvent =
| { ev: "devices"; devices: string[]; error?: string }
| { ev: "wireless"; ok: boolean; error?: string }
| { ev: "status"; text: string; err: boolean }
| { ev: "started"; label: string; port: string }
| { ev: "stopped"; error?: string }

export interface StartParams {
    mode: Mode
    serial?: string
    ip?: string
    port: string
    profile: number
    silence: boolean
}

/** Thin wrapper around the Go engine process (newline-delimited JSON on stdio). */
export class Engine {
    private proc: Subprocess<"pipe", "pipe", "inherit">
        private closed = false

        constructor(
            binary: string,
            private onEvent: (e: EngineEvent) => void,
                private onExit: (code: number | null) => void,
        ) {
            this.proc = spawn([binary], { stdin: "pipe", stdout: "pipe", stderr: "inherit" })
            void this.readLoop()
            void this.proc.exited.then((code) => {
                this.closed = true
                this.onExit(code)
            })
        }

        private async readLoop() {
            const decoder = new TextDecoder()
            let buf = ""
            for await (const chunk of this.proc.stdout) {
                buf += decoder.decode(chunk, { stream: true })
                let nl: number
                while ((nl = buf.indexOf("\n")) >= 0) {
                    const line = buf.slice(0, nl).trim()
                    buf = buf.slice(nl + 1)
                    if (!line) continue
                        try {
                            this.onEvent(JSON.parse(line) as EngineEvent)
                        } catch {
                            // ignore malformed lines; stdout is protocol-only so this is unexpected
                        }
                }
            }
        }

        private send(cmd: Record<string, unknown>) {
            if (this.closed) return
                this.proc.stdin.write(JSON.stringify(cmd) + "\n")
                this.proc.stdin.flush()
        }

        listDevices() {
            this.send({ op: "list_devices" })
        }
        connectWireless(addr: string) {
            this.send({ op: "connect_wireless", addr })
        }
        start(p: StartParams) {
            this.send({ op: "start", ...p })
        }
        stop() {
            this.send({ op: "stop" })
        }

        /** Ask the engine to tear down (stop stream, remove adb tunnel) and wait for it. */
        async quit(timeoutMs = 1500) {
            if (this.closed) return
                this.send({ op: "quit" })
                try {
                    this.proc.stdin.end()
                } catch {}
                const timer = setTimeout(() => this.proc.kill(), timeoutMs)
                await this.proc.exited
                clearTimeout(timer)
        }
}
