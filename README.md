# Sonidex

Sonidex is an ultra-low-latency desktop-to-Android audio bridge that streams uncompressed 16-bit 48 kHz stereo PCM audio directly to your mobile device, with ~10–15 ms latency over a physical USB connection using automated ADB tunneling.

Wireless ADB and direct WiFi connections are also supported for cable-free setups, at the cost of some added latency and jitter compared to USB.

The Linux and Windows builds are the **streamer** (capture system audio and send it). The Android build is the **receiver** (listen and play it).
---

### Screenshots
To be updated soon
---

### Features
* **Zero Terminal Setup:** GUI-based; automatically discovers connected devices and provisions ADB tunnels behind the scenes.
* **Lossless PCM:** Raw 48 kHz stereo streaming without compression overhead or fidelity loss.
* **Cross-platform UI:** Lightweight Fyne GUI running natively across platforms (Windows/Linux) and display servers (Xorg/XLibre/Wayland), with an optional software-rendering mode that avoids GPU usage.
* **Terminal UI option:** An alternative to the GUI — a [Bubble Tea](https://github.com/charmbracelet/bubbletea)-based TUI streamer for Linux that talks to the same backend, with no GUI toolkit dependency.
* **Small RAM Footprint:** Efficient Go-based runtime.
---

### Downloads
Grab the binary for your platform — no local Go toolchain or compiling needed.
* `sonidex-windows-amd64.exe`: GUI streamer, run on Windows.
* `sonidex-linux-amd64`: GUI streamer, run on Xorg/XLibre on Linux.
* `sonidex-linux-wayland-amd64`: GUI streamer built with the `wayland` tag for native Wayland sessions.
* `sonidex-tui-linux-amd64`: terminal streamer for Linux — same streaming backend, no GUI toolkit.
* `Sonidex.apk`: receiver, install on the Android phone.
---

### Prerequisites
* **Host PC:** `adb` (Android Debug Bridge) installed in system `$PATH`.
* **Android:** Enable **USB Debugging** in *Developer Options*.
* **Linux hosts:** PulseAudio or PipeWire (with its Pulse shim) running, with a `Monitor of ...` source available for your output device — this is what loopback capture reads from. Check with `pactl list sources short`. Plain ALSA-only setups with no Pulse/PipeWire server aren't supported for loopback capture. musl-based distros (e.g. Alpine) need a glibc compat layer (such as `gcompat`) to run the prebuilt binary, or should build from source.
---

### Usage
1. Connect your Android phone to the PC via USB.
2. Launch **Sonidex Receiver** on Android and tap **Start Receiving**.
3. Launch **Sonidex Streamer** on the PC, pick your phone under **Android Target Device** (use **Refresh Devices** if the list is empty), and tap **Start Streaming**.
---

### Settings
* **Disable GPU (software rendering):** checkbox available in every build (PC streamer and Android receiver). When enabled, the app forces CPU-based rendering via `LIBGL_ALWAYS_SOFTWARE=1` and `GALLIUM_DRIVER=llvmpipe` instead of using the GPU. The choice is saved in the app preferences. Changing it requires an app restart to take full effect, since the rendering path is fixed at startup.
* You can also force software rendering before first launch with the environment variable `SONIDEX_NO_GPU=1`.
---

### Credits
* UI Framework: [Fyne](https://fyne.io/) (MPL-2.0)
* TUI Framework: [Bubble Tea](https://github.com/charmbracelet/bubbletea), [Bubbles](https://github.com/charmbracelet/bubbles), [Lip Gloss](https://github.com/charmbracelet/lipgloss) (MIT)
* Audio Backend: [malgo / miniaudio](https://github.com/gen2brain/malgo) (MIT)
