package com.local.sonidex;

/**
 * Real-time analysis of the PCM the {@link Receiver} is about to play: overall
 * level, bass, treble, a downsampled waveform and a beat detector.
 *
 * Because the receiver already owns the raw samples, nothing here needs the
 * RECORD_AUDIO permission or android.media.audiofx.Visualizer, and what is
 * analysed is exactly what is played.
 *
 * Cost model (this is the battery story):
 *  - {@link #enabled} is false whenever nothing is on screen, and then the
 *    audio thread does not call {@link #feed} at all: zero overhead.
 *  - When enabled it is a handful of float ops per sample (~3 k per 10 ms
 *    chunk), no allocation, no FFT.
 *  - The waveform snapshot is only built while {@link #wantWave} is true.
 *
 * Pure Java on purpose (no android.* imports) so it can be unit-tested on a JVM.
 */
final class Analyzer {
    /** Called on the audio thread when sound starts again after a quiet spell. */
    interface Listener {
        void onAudioResumed();
    }

    static final int WAVE_POINTS = 128;
    /** Quiet for this long = "no music": the view stops redrawing. */
    static final long QUIET_NS = 1500000000L;

    private static final int WIN = 2048; // mono window for the waveform (~43 ms), power of two
    private static final int STEP = WIN / WAVE_POINTS;
    private static final float SILENCE_RMS = 0.0006f; // about -64 dBFS

    // One-pole low-pass coefficients, a = 1 - exp(-2*pi*fc/fs) at 48 kHz.
    private static final float A_BASS = 0.0207f; // fc ~160 Hz, cascaded twice (12 dB/oct, ~100 Hz corner)
    private static final float A_MID = 0.2305f;  // fc ~2 kHz: treble = signal - this

    // Beat detector tuning. Chunks are ~10 ms, so 26 chunks ~ 260 ms (max ~230 BPM).
    private static final int MIN_BEAT_GAP = 26;
    private static final float FAST = 0.4f;      // ~25 ms envelope
    private static final float SLOW = 0.015f;    // ~0.7 s background level
    private static final float RATIO = 1.35f;    // fast must exceed slow by 35 %
    private static final float MARGIN = 0.003f;
    private static final float FLOOR = 0.008f;   // ignore bass this weak

    /** Set by the UI. When false the receiver does not call feed(). */
    volatile boolean enabled;
    /** Set by the UI when the Wave visual is showing. */
    volatile boolean wantWave;
    volatile Listener listener;

    // ---- outputs (written by the audio thread, read by the UI thread) ----
    volatile float rms;
    volatile float bass;
    volatile float treble;
    /** Incremented once per detected beat; compare with the last value you saw. */
    volatile int beatCount;
    volatile float beatStrength; // 0.35 .. 1
    volatile long beatNanos;     // System.nanoTime() at detection
    volatile long lastLoudNanos;
    private volatile boolean everLoud;

    private final Object waveLock = new Object();
    private final float[] wave = new float[WAVE_POINTS];

    // ---- audio-thread state ----
    private final float[] mono = new float[WIN];
    private int widx;
    private float lp1, lp2, mid;
    private float fast, slow, prevFast;
    private int chunks;
    private int warm; // chunks since sound (re)started: the background level is a plain mean until it fills
    private int lastBeatChunk = -MIN_BEAT_GAP;

    /** True while sound has been heard within the last {@link #QUIET_NS}. */
    boolean isLive(long nowNanos) {
        return everLoud && nowNanos - lastLoudNanos < QUIET_NS;
    }

    /** Copies the latest waveform (mono, -1..1) into out (length >= WAVE_POINTS). */
    void copyWave(float[] out) {
        synchronized (waveLock) {
            System.arraycopy(wave, 0, out, 0, WAVE_POINTS);
        }
    }

    /**
     * Analyse one chunk of 16-bit little-endian stereo PCM. Audio thread only.
     * @param len number of valid bytes (whole 4-byte frames)
     */
    void feed(byte[] pcm, int len) {
        final int frames = len >> 2;
        if (frames <= 0) {
            return;
        }
        final boolean wantWaveNow = wantWave;
        float sumAll = 0, sumBass = 0, sumTreb = 0;
        float a = lp1, b = lp2, m1 = mid;
        int w = widx;
        for (int i = 0, p = 0; i < frames; i++, p += 4) {
            int l = (pcm[p] & 0xff) | (pcm[p + 1] << 8); // high byte is signed -> sign-extends
            int r = (pcm[p + 2] & 0xff) | (pcm[p + 3] << 8);
            float m = (l + r) * (1f / 65536f); // (l+r)/2 / 32768
            a += A_BASS * (m - a);
            b += A_BASS * (a - b);
            m1 += A_MID * (m - m1);
            float t = m - m1;
            sumAll += m * m;
            sumBass += b * b;
            sumTreb += t * t;
            if (wantWaveNow) {
                mono[w] = m;
                w = (w + 1) & (WIN - 1);
            }
        }
        lp1 = a;
        lp2 = b;
        mid = m1;
        float inv = 1f / frames;
        float r = (float) Math.sqrt(sumAll * inv);
        float bs = (float) Math.sqrt(sumBass * inv);
        float tr = (float) Math.sqrt(sumTreb * inv);
        rms = r;
        bass = bs;
        treble = tr;

        if (wantWaveNow) {
            widx = w;
            synchronized (waveLock) {
                for (int k = 0; k < WAVE_POINTS; k++) {
                    float s = 0;
                    int base = w + k * STEP; // oldest sample sits at the write index
                    for (int j = 0; j < STEP; j++) {
                        s += mono[(base + j) & (WIN - 1)];
                    }
                    wave[k] = s * (1f / STEP);
                }
            }
        }

        final long now = System.nanoTime();
        if (r > SILENCE_RMS) {
            boolean resumed = !everLoud || now - lastLoudNanos > QUIET_NS;
            lastLoudNanos = now;
            everLoud = true;
            if (resumed) {
                fast = slow = bs; // don't fire a phantom beat on the first chunk
                prevFast = bs;
                warm = 0;
                Listener li = listener;
                if (li != null) {
                    li.onAudioResumed();
                }
            }
        }

        chunks++;
        fast += FAST * (bs - fast);
        // Cumulative mean for the first ~65 chunks, then the 0.7 s moving average. Without this,
        // a loud first chunk would inflate the baseline and hide the next second of kicks.
        float k = 1f / (warm + 1);
        slow += (k > SLOW ? k : SLOW) * (bs - slow);
        if (warm < 1000) {
            warm++;
        }
        if (fast > slow * RATIO + MARGIN && fast > FLOOR && fast > prevFast
                && chunks - lastBeatChunk >= MIN_BEAT_GAP) {
            lastBeatChunk = chunks;
            float s = fast / (slow + 1e-6f) - 1f;
            beatStrength = s < 0.35f ? 0.35f : (s > 1f ? 1f : s);
            beatNanos = now;
            beatCount++; // volatile write last: readers see strength/time first
        }
        prevFast = fast;
    }
}
