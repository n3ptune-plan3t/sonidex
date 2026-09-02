# Sonidex

**Sonidex** is an ultra-low-latency desktop-to-Android audio bridge designed to run directly over a physical USB connection using automated ADB reverse tunneling. It eliminates wireless jitter and streams uncompressed 16-bit 48 kHz stereo PCM audio directly to your mobile device with ~10–15 ms latency.

---

### Features
* **Zero Terminal Setup:** Automatically discovers connected devices and provisions ADB reverse tunnels behind the scenes.
* **Lossless PCM:** Raw 48 kHz stereo streaming without compression overhead or fidelity loss.
* **Hardware Accelerated UI:** Lightweight Fyne GUI running natively across platforms.
* **Sub-50MB RAM Footprint:** Efficient Go-based runtime.

---

### Prerequisites
* **Host PC:** `adb` (Android Debug Bridge) installed in system `$PATH`.
* **Android:** Enable **USB Debugging** in *Developer Options*.

---

### Usage
1. Connect your Android phone to the PC via USB.
2. Launch **Sonidex** on Android and tap **Start Receiving Audio**.
3. Launch **Sonidex** on the PC, choose your device under **Android Target Device**, and tap **Connect & Stream Audio**.

---

### Credits
* UI Framework: [Fyne](https://fyne.io/) (MPL-2.0)
* Audio Backend: [malgo / miniaudio](https://github.com/gen2brain/malgo) (MIT)
* Application Icon: [Font Awesome](https://fontawesome.com/) (CC BY 4.0)
