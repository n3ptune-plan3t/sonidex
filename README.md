# Sonidex

**Sonidex** is an ultra-low-latency desktop-to-Android audio bridge designed to run directly over a physical USB connection using automated ADB tunneling. It eliminates wireless jitter and streams uncompressed 16-bit 48 kHz stereo PCM audio directly to your mobile device with ~10–15 ms latency.

The Linux and Windows builds are the **streamer** (capture system audio and send it). The Android build is the **receiver** (listen and play it).

---

### Features
* **Zero Terminal Setup:** Automatically discovers connected devices and provisions ADB tunnels behind the scenes.
* **Lossless PCM:** Raw 48 kHz stereo streaming without compression overhead or fidelity loss.
* **Cross-platform UI:** Lightweight Fyne GUI running natively across platforms, with an optional software-rendering mode that avoids GPU usage.
* **Sub-50MB RAM Footprint:** Efficient Go-based runtime.

---

### Downloads

Grab the binary for your platform — no local Go toolchain or compiling needed.

* `sonidex-linux-amd64` / `sonidex-windows-amd64.exe`: streamer, run on the PC.
* Sonidex `.apk`: receiver, install on the Android phone.

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

### Recent fixes
* **ADB tunnel direction:** the PC dials `127.0.0.1:<port>` and the phone listens, so tunnel setup now uses `adb forward` instead of `adb reverse` — the old direction left nothing listening on the host side and every connection attempt failed.
* **Jitter buffer cap:** `AudioBuffer.Push` now actually enforces its size ceiling (append, then trim from the front) instead of a net-zero trim/append that let buffered latency grow unbounded over a session.
* **Linux/BSD loopback capture:** `malgo.Loopback` only works via WASAPI on Windows. On Linux the app now enumerates capture devices and selects the PulseAudio/PipeWire `Monitor of ...` source explicitly instead of relying on a loopback device type that doesn't exist there.
* **Role split:** Linux/Windows builds now open the streamer UI (system-audio capture, ADB forward, TCP send) while the Android build opens the receiver UI (TCP listen plus local playback), instead of every build opening the receiver.
* **Receiver playback:** the Android side now actually plays the buffered stream through the default output device instead of receiving into a buffer that was never drained.

---

### Credits
* UI Framework: [Fyne](https://fyne.io/) (MPL-2.0)
* Audio Backend: [malgo / miniaudio](https://github.com/gen2brain/malgo) (MIT)
* Application Icon: [Font Awesome](https://fontawesome.com/) (CC BY 4.0)
