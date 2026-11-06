package com.local.sonidex;

import android.content.Context;
import android.graphics.Bitmap;
import android.graphics.Canvas;
import android.graphics.Color;
import android.graphics.Paint;
import android.graphics.PorterDuff;
import android.graphics.PorterDuffXfermode;
import android.graphics.RadialGradient;
import android.graphics.RectF;
import android.graphics.Shader;
import android.os.PowerManager;
import android.view.View;

import java.util.Random;

/**
 * Optional full-screen background visuals (Cosmos, Fireflies, Wave) driven by
 * the audio the receiver is playing, with beat sync.
 *
 * Battery rules, all enforced in {@link #refresh()} and {@link #step()}:
 *  - Runs only while mode != OFF, the activity is resumed (so: not while the
 *    screen is off and not while another app is in front), the view is
 *    attached and its window is visible. Otherwise no frame callbacks are
 *    scheduled and {@link Analyzer#enabled} is false, so the audio thread
 *    skips analysis too.
 *  - Capped at ~30 fps (~20 fps in the system's battery saver) using delayed
 *    vsync callbacks, so the CPU/GPU idle between frames.
 *  - When the music stops, the visual fades out, draws once more and then
 *    stops scheduling frames entirely until the analyzer reports sound again.
 *  - No allocation while drawing; a handful of small glow sprites are built
 *    once per mode (about 16 KB each). Dark backgrounds are also kind to OLEDs.
 */
final class VisualizerView extends View implements Analyzer.Listener {
    static final int OFF = 0;
    static final int COSMOS = 1;
    static final int FIREFLIES = 2;
    static final int WAVE = 3;
    static final int MODES = 4;

    static String modeName(int m) {
        switch (m) {
            case COSMOS:
                return "Cosmos";
            case FIREFLIES:
                return "Fireflies";
            case WAVE:
                return "Wave";
            default:
                return "Off";
        }
    }

    /**
     * Sound reaches the speaker a little after the receiver hands it to the
     * AudioTrack (its buffer plus the output path), so the beat pulse is held
     * back by this much to land with what you hear. Raise it if visuals feel
     * early, lower it if they feel late.
     */
    private static final long BEAT_DELAY_NS = 45000000L;
    /** Keep drawing this long after the last sound so the visual can fade out. */
    private static final long SETTLE_NS = 3000000000L;
    private static final float TWO_PI = 6.2831855f;

    private final Analyzer an = new Analyzer();
    private final PowerManager pm;
    private final float den;
    private final Random rnd = new Random();

    // ---- lifecycle / scheduling ----
    private int mode = OFF;
    private int builtFor = -1;
    private boolean resumed;
    private boolean attached;
    private boolean ticking;
    private boolean lowPower;
    private int intervalMs = 33;
    private long lastNs;

    private final Runnable tick = new Runnable() {
        public void run() {
            step();
        }
    };
    private final Runnable wake = new Runnable() {
        public void run() {
            if (!ticking && shouldRun()) {
                startTicking();
            }
        }
    };

    // ---- geometry ----
    private int W, H;
    private float cx, cy, maxDim, focal;

    // ---- drawing tools (allocated once) ----
    private final Paint glow = new Paint(Paint.FILTER_BITMAP_FLAG);
    private final Paint line = new Paint(Paint.ANTI_ALIAS_FLAG);
    private final Paint ringPaint = new Paint(Paint.ANTI_ALIAS_FLAG);
    private final RectF dst = new RectF();
    private final float[] hsv = new float[3];
    private Bitmap[] spr = new Bitmap[0];

    // ---- smoothed audio features, shared by all modes ----
    private float pkR, pkB, pkT;      // peak followers for auto-gain
    private float lvl, bas, tre;      // 0..1 smoothed level / bass / treble
    private float pulse;              // 1 on a beat, decays quickly
    private int seenBeat;
    private boolean pending;
    private long pendAt;
    private float pendStr;
    private int beatSeq;              // counts beats that reached the screen
    private boolean beatNow;          // true only in the frame a beat lands
    private float time;
    private float hue = 220f;
    private boolean ringOn;
    private float ringT, ringStr;

    // ---- cosmos ----
    private static final int MAX_STARS = 150;
    private final float[] stX = new float[MAX_STARS];
    private final float[] stY = new float[MAX_STARS];
    private final float[] stZ = new float[MAX_STARS];
    private final float[] stSX = new float[MAX_STARS];
    private final float[] stSY = new float[MAX_STARS];
    private final float[] stPX = new float[MAX_STARS];
    private final float[] stPY = new float[MAX_STARS];
    private final byte[] stC = new byte[MAX_STARS];
    private static final int[] STAR_COLORS = {0xFFFFFFFF, 0xFFAFC8FF, 0xFFD2B0FF, 0xFFFFE0C0};
    private static final int[] NEBULA_COLORS = {0xFF5B3DF5, 0xFFD83DA8, 0xFF2AA7E0};
    private int starCount;
    private float cosmosSpeed;

    // ---- fireflies ----
    private static final int MAX_FLIES = 48;
    private final float[] fX = new float[MAX_FLIES];
    private final float[] fY = new float[MAX_FLIES];
    private final float[] fAng = new float[MAX_FLIES];
    private final float[] fSpd = new float[MAX_FLIES];
    private final float[] fPh = new float[MAX_FLIES];
    private final float[] fRate = new float[MAX_FLIES];
    private final float[] fSize = new float[MAX_FLIES];
    private final float[] fFlash = new float[MAX_FLIES];
    private final byte[] fCol = new byte[MAX_FLIES];
    private static final int[] FLY_COLORS = {0xFFE8FF5A, 0xFFFFD25A, 0xFF9BFF7A, 0xFFFFF3B0};
    private int flyCount;

    // ---- wave ----
    private static final int WN = Analyzer.WAVE_POINTS;
    private final float[] wv = new float[WN];
    private final float[] wy = new float[WN];
    private final float[] wpts = new float[(WN - 1) * 4];
    private float wvPeak;

    VisualizerView(Context c) {
        super(c);
        pm = (PowerManager) c.getSystemService(Context.POWER_SERVICE);
        den = c.getResources().getDisplayMetrics().density;
        glow.setXfermode(new PorterDuffXfermode(PorterDuff.Mode.ADD));
        line.setStyle(Paint.Style.STROKE);
        line.setStrokeCap(Paint.Cap.ROUND);
        ringPaint.setStyle(Paint.Style.STROKE);
        setVisibility(GONE);
        setImportantForAccessibility(IMPORTANT_FOR_ACCESSIBILITY_NO); // decorative
    }

    // =====================================================================
    // Public control
    // =====================================================================

    Analyzer analyzer() {
        return an;
    }

    int mode() {
        return mode;
    }

    void setMode(int m) {
        if (m == mode) {
            return;
        }
        mode = m;
        builtFor = -1;
        setVisibility(m == OFF ? GONE : VISIBLE);
        refresh();
    }

    /** Call from Activity.onResume (true) and onPause (false). */
    void setResumed(boolean r) {
        resumed = r;
        refresh();
    }

    // =====================================================================
    // Run/stop logic
    // =====================================================================

    private boolean shouldRun() {
        return mode != OFF && resumed && attached && getWindowVisibility() == VISIBLE;
    }

    private void refresh() {
        final boolean run = shouldRun();
        an.enabled = run;
        an.wantWave = run && mode == WAVE;
        an.listener = run ? this : null;
        if (run) {
            lowPower = pm != null && pm.isPowerSaveMode();
            intervalMs = lowPower ? 50 : 33;
            if (builtFor != mode) {
                build();
            }
            if (!ticking) {
                startTicking(); // draws at least one frame, then sleeps if it is quiet
            }
        } else {
            ticking = false;
            removeCallbacks(tick);
            removeCallbacks(wake);
        }
    }

    private void startTicking() {
        ticking = true;
        lastNs = 0;
        seenBeat = an.beatCount;
        pending = false;
        postOnAnimation(tick);
    }

    /** Audio thread: sound just started again after a quiet spell. */
    public void onAudioResumed() {
        post(wake);
    }

    @Override
    protected void onAttachedToWindow() {
        super.onAttachedToWindow();
        attached = true;
        refresh();
    }

    @Override
    protected void onDetachedFromWindow() {
        attached = false;
        refresh();
        super.onDetachedFromWindow();
    }

    @Override
    protected void onWindowVisibilityChanged(int visibility) {
        super.onWindowVisibilityChanged(visibility);
        refresh();
    }

    @Override
    protected void onSizeChanged(int w, int h, int ow, int oh) {
        super.onSizeChanged(w, h, ow, oh);
        W = w;
        H = h;
        cx = w * 0.5f;
        cy = h * 0.5f;
        maxDim = Math.max(w, h);
        focal = 0.30f * maxDim;
        builtFor = -1;
        if (shouldRun()) {
            build();
            invalidate();
        }
    }

    // =====================================================================
    // Frame loop
    // =====================================================================

    private void step() {
        if (!ticking) {
            return;
        }
        final long now = System.nanoTime();
        float dt = lastNs == 0 ? 0.033f : (now - lastNs) * 1e-9f;
        if (dt > 0.1f) {
            dt = 0.1f; // e.g. after a hiccup: don't teleport things
        }
        lastNs = now;
        time += dt;
        if (time > 3600f) {
            time -= 3600f; // keep float precision for the sin() animations
        }

        sense(dt, now);
        switch (mode) {
            case COSMOS:
                advanceCosmos(dt);
                break;
            case FIREFLIES:
                advanceFireflies(dt);
                break;
            case WAVE:
                advanceWave(dt);
                break;
            default:
                break;
        }
        invalidate();

        final long lastLoud = an.lastLoudNanos;
        if (an.isLive(now) || (lastLoud != 0 && now - lastLoud < SETTLE_NS)) {
            // Fire on the vsync just before the next frame is due. The -12 ms
            // slack makes a 33 ms interval land on every second 60 Hz vsync.
            postOnAnimationDelayed(tick, Math.max(1, intervalMs - 12));
        } else {
            ticking = false; // asleep: Analyzer#onAudioResumed (or a resume) wakes us
        }
    }

    private static float follow(float cur, float target, float dt, float up, float down) {
        float k = (target > cur ? up : down) * dt;
        if (k > 1f) {
            k = 1f;
        }
        return cur + (target - cur) * k;
    }

    /** Turns the analyzer's raw numbers into smooth 0..1 controls and beat pulses. */
    private void sense(float dt, long now) {
        final boolean live = an.isLive(now);
        float r = 0, b = 0, t = 0;
        if (live) { // the analyzer keeps its last values while silent: don't freeze on them
            r = an.rms;
            b = an.bass;
            t = an.treble;
        }
        pkR = Math.max(r, pkR - pkR * 0.12f * dt); // auto-gain: peaks fall ~12 % per second
        pkB = Math.max(b, pkB - pkB * 0.12f * dt);
        pkT = Math.max(t, pkT - pkT * 0.12f * dt);
        lvl = follow(lvl, r / Math.max(pkR, 0.04f), dt, 18f, 3f);
        bas = follow(bas, b / Math.max(pkB, 0.015f), dt, 25f, 5f);
        tre = follow(tre, t / Math.max(pkT, 0.010f), dt, 25f, 6f);

        final int bc = an.beatCount;
        if (bc != seenBeat) {
            seenBeat = bc;
            pending = true;
            pendAt = an.beatNanos + BEAT_DELAY_NS;
            pendStr = an.beatStrength;
        }
        beatNow = false;
        if (pending && now >= pendAt) {
            pending = false;
            beatNow = true;
            beatSeq++;
            pulse = Math.max(pulse, 0.55f + 0.45f * pendStr);
            ringOn = true;
            ringT = 0f;
            ringStr = pendStr;
            hue += 38f;
        }
        pulse -= pulse * Math.min(1f, dt * 5.5f);
        hue += dt * 9f;
        if (hue >= 360f) {
            hue -= 360f;
        }
    }

    // =====================================================================
    // Build (per mode, once)
    // =====================================================================

    private void build() {
        if (W == 0 || H == 0 || mode == OFF) {
            return;
        }
        // Old sprites are just dropped (not recycle()d): a hardware frame may still
        // reference them, and they are only ~16 KB each.
        switch (mode) {
            case COSMOS:
                spr = new Bitmap[STAR_COLORS.length + NEBULA_COLORS.length];
                for (int i = 0; i < STAR_COLORS.length; i++) {
                    spr[i] = makeGlow(STAR_COLORS[i], true);
                }
                for (int i = 0; i < NEBULA_COLORS.length; i++) {
                    spr[STAR_COLORS.length + i] = makeGlow(NEBULA_COLORS[i], false);
                }
                starCount = lowPower ? 80 : MAX_STARS;
                for (int i = 0; i < starCount; i++) {
                    spawnStar(i, true);
                }
                break;
            case FIREFLIES:
                spr = new Bitmap[FLY_COLORS.length];
                for (int i = 0; i < FLY_COLORS.length; i++) {
                    spr[i] = makeGlow(FLY_COLORS[i], true);
                }
                flyCount = lowPower ? 28 : MAX_FLIES;
                for (int i = 0; i < flyCount; i++) {
                    fX[i] = rnd.nextFloat() * W;
                    fY[i] = rnd.nextFloat() * H;
                    fAng[i] = rnd.nextFloat() * TWO_PI;
                    fSpd[i] = 0.6f + 0.8f * rnd.nextFloat();
                    fPh[i] = rnd.nextFloat() * TWO_PI;
                    fRate[i] = 1.0f + 2.0f * rnd.nextFloat();
                    fSize[i] = rnd.nextFloat();
                    fFlash[i] = 0f;
                    fCol[i] = (byte) rnd.nextInt(FLY_COLORS.length);
                }
                break;
            default: // WAVE
                spr = new Bitmap[]{makeGlow(0xFF4A3AFF, false)};
                wvPeak = 0f;
                break;
        }
        builtFor = mode;
    }

    /** A 64x64 radial sprite in the given colour; {@code core} adds a bright centre (a "point of light"). */
    private static Bitmap makeGlow(int rgb, boolean core) {
        final int s = 64;
        final int r = Color.red(rgb), g = Color.green(rgb), b = Color.blue(rgb);
        int[] colors;
        float[] stops;
        if (core) {
            colors = new int[]{Color.argb(255, r, g, b), Color.argb(235, r, g, b),
                    Color.argb(70, r, g, b), Color.argb(0, r, g, b)};
            stops = new float[]{0f, 0.16f, 0.42f, 1f};
        } else {
            colors = new int[]{Color.argb(200, r, g, b), Color.argb(80, r, g, b), Color.argb(0, r, g, b)};
            stops = new float[]{0f, 0.45f, 1f};
        }
        Bitmap bm = Bitmap.createBitmap(s, s, Bitmap.Config.ARGB_8888);
        Paint p = new Paint(Paint.ANTI_ALIAS_FLAG);
        p.setShader(new RadialGradient(s / 2f, s / 2f, s / 2f, colors, stops, Shader.TileMode.CLAMP));
        new Canvas(bm).drawCircle(s / 2f, s / 2f, s / 2f, p);
        return bm;
    }

    // =====================================================================
    // COSMOS: a starfield you fly through. Level and bass set the speed,
    // each beat is a surge of warp plus an expanding ring, and soft nebulae
    // breathe with the bass.
    // =====================================================================

    private void spawnStar(int i, boolean anywhere) {
        float ang = rnd.nextFloat() * TWO_PI;
        float rad = 0.04f + 0.96f * (float) Math.sqrt(rnd.nextFloat());
        stX[i] = rad * (float) Math.cos(ang);
        stY[i] = rad * (float) Math.sin(ang);
        stZ[i] = anywhere ? 0.08f + 0.92f * rnd.nextFloat() : 0.85f + 0.15f * rnd.nextFloat();
        stC[i] = (byte) rnd.nextInt(STAR_COLORS.length);
        float k = focal / stZ[i];
        stSX[i] = cx + stX[i] * k;
        stSY[i] = cy + stY[i] * k;
        stPX[i] = stSX[i];
        stPY[i] = stSY[i];
    }

    private void advanceCosmos(float dt) {
        cosmosSpeed = 0.04f + 0.26f * lvl + 0.45f * bas + 1.0f * pulse;
        final float m = den * 24f;
        for (int i = 0; i < starCount; i++) {
            stPX[i] = stSX[i];
            stPY[i] = stSY[i];
            stZ[i] -= cosmosSpeed * dt;
            if (stZ[i] < 0.04f) {
                spawnStar(i, false);
                continue;
            }
            float k = focal / stZ[i];
            stSX[i] = cx + stX[i] * k;
            stSY[i] = cy + stY[i] * k;
            if (stSX[i] < -m || stSX[i] > W + m || stSY[i] < -m || stSY[i] > H + m) {
                spawnStar(i, false);
            }
        }
        if (ringOn) {
            ringT += dt * 1.6f;
            if (ringT >= 1f) {
                ringOn = false;
            }
        }
    }

    private void drawCosmos(Canvas c) {
        c.drawColor(0xFF04050C);
        final int nb = STAR_COLORS.length;
        for (int k = 0; k < NEBULA_COLORS.length; k++) { // slow drifting nebulae
            float a = time * (0.03f + 0.015f * k) + k * 2.1f;
            float x = cx + (float) Math.cos(a) * W * 0.28f;
            float y = cy + (float) Math.sin(a * 1.3f) * H * 0.22f;
            float half = maxDim * (0.45f + 0.08f * k + 0.12f * bas + 0.10f * pulse);
            float alpha = 0.10f + 0.20f * bas + 0.30f * pulse;
            glow.setAlpha((int) (255 * Math.min(0.7f, alpha)));
            dst.set(x - half, y - half, x + half, y + half);
            c.drawBitmap(spr[nb + k], null, dst, glow);
        }
        final boolean streaks = cosmosSpeed > 0.30f;
        for (int i = 0; i < starCount; i++) {
            float near = 1f - stZ[i];
            float half = den * (2.5f + 9f * near * near);
            int alpha = (int) (255 * Math.min(1f, 0.15f + 1.2f * near));
            if (streaks) {
                line.setColor(STAR_COLORS[stC[i]]);
                line.setAlpha(alpha * 6 / 10);
                line.setStrokeWidth(half * 0.5f);
                c.drawLine(stPX[i], stPY[i], stSX[i], stSY[i], line);
            }
            glow.setAlpha(alpha);
            dst.set(stSX[i] - half, stSY[i] - half, stSX[i] + half, stSY[i] + half);
            c.drawBitmap(spr[stC[i]], null, dst, glow);
        }
        if (ringOn) {
            float k = 1f - ringT;
            ringPaint.setColor(0xFF9EB8FF);
            ringPaint.setAlpha((int) (200 * k * k * ringStr));
            ringPaint.setStrokeWidth(den * (1f + 5f * k));
            c.drawCircle(cx, cy, ringT * maxDim * 0.55f, ringPaint);
        }
    }

    // =====================================================================
    // FIREFLIES: slow wandering lights that blink on their own; every beat
    // makes most of them flare and dart, and the treble adds sparkle.
    // =====================================================================

    private void advanceFireflies(float dt) {
        final float base = den * (10f + 18f * lvl);
        final float m = den * 24f;
        for (int i = 0; i < flyCount; i++) {
            if (beatNow && ((i * 7 + beatSeq * 5) % 10) < 6) { // ~60 % of them flare, a different set each beat
                fFlash[i] = Math.max(fFlash[i], pulse);
            }
            fAng[i] += (float) Math.sin(time * 0.7f * fRate[i] + fPh[i]) * 1.6f * dt;
            float sp = base * fSpd[i] * (1f + 2.5f * fFlash[i]);
            fX[i] += (float) Math.cos(fAng[i]) * sp * dt;
            fY[i] += (float) Math.sin(fAng[i]) * sp * dt;
            if (fX[i] < -m) {
                fX[i] += W + 2 * m;
            } else if (fX[i] > W + m) {
                fX[i] -= W + 2 * m;
            }
            if (fY[i] < -m) {
                fY[i] += H + 2 * m;
            } else if (fY[i] > H + m) {
                fY[i] -= H + 2 * m;
            }
            fFlash[i] -= fFlash[i] * Math.min(1f, dt * 3.2f);
        }
    }

    private void drawFireflies(Canvas c) {
        c.drawColor(0xFF030705);
        for (int i = 0; i < flyCount; i++) {
            float blink = 0.5f + 0.5f * (float) Math.sin(time * fRate[i] + fPh[i]);
            float sparkle = 0.2f * tre * (0.5f + 0.5f * (float) Math.sin(time * 9f + fPh[i] * 3f));
            float b = 0.22f + 0.28f * blink + 0.65f * fFlash[i] + sparkle;
            if (b > 1f) {
                b = 1f;
            }
            float half = den * (9f + 7f * fSize[i]) * (0.8f + 0.5f * lvl + 0.7f * fFlash[i]);
            glow.setAlpha((int) (255 * b));
            dst.set(fX[i] - half, fY[i] - half, fX[i] + half, fY[i] + half);
            c.drawBitmap(spr[fCol[i]], null, dst, glow);
        }
    }

    // =====================================================================
    // WAVE: five layered lines across the screen. The middle one is the real
    // waveform; the others blend it with gentle sines. Colours drift and jump
    // on the beat, and line weight / background flash with it.
    // =====================================================================

    private void advanceWave(float dt) {
        an.copyWave(wv);
        float pk = 0;
        for (int i = 0; i < WN; i++) {
            float a = Math.abs(wv[i]);
            if (a > pk) {
                pk = a;
            }
        }
        wvPeak = Math.max(pk, wvPeak - wvPeak * 0.5f * dt);
    }

    private void drawWave(Canvas c) {
        c.drawColor(Color.rgb(5 + (int) (8 * pulse), 7 + (int) (8 * pulse), 14 + (int) (22 * pulse)));
        float ga = 0.10f + 0.30f * pulse + 0.15f * lvl;
        glow.setAlpha((int) (255 * Math.min(0.6f, ga)));
        float half = maxDim * (0.55f + 0.2f * pulse);
        dst.set(cx - half, cy - half, cx + half, cy + half);
        c.drawBitmap(spr[0], null, dst, glow);

        final float gain = 1f / Math.max(wvPeak, 0.03f);
        final float dx = W / (float) (WN - 1);
        for (int k = 0; k < 5; k++) {
            int kk = k - 2;
            int akk = kk < 0 ? -kk : kk;
            float y0 = H * (0.5f + kk * 0.16f);
            float amp = H * 0.11f * (1f - 0.14f * akk) * (0.12f + 0.88f * lvl + 0.6f * pulse);
            float f = 0.16f + 0.05f * akk;
            float sp = 1.4f + 0.5f * kk;
            for (int i = 0; i < WN; i++) {
                int j = i - kk * 3;
                j = j < 0 ? 0 : (j > WN - 1 ? WN - 1 : j);
                float s = wv[j] * gain;
                s = s < -1.3f ? -1.3f : (s > 1.3f ? 1.3f : s);
                float osc = (float) Math.sin(i * f + time * sp);
                wy[i] = y0 + amp * (0.8f * s + 0.3f * osc * (0.3f + lvl));
            }
            int p = 0;
            for (int i = 0; i < WN - 1; i++) {
                wpts[p++] = i * dx;
                wpts[p++] = wy[i];
                wpts[p++] = (i + 1) * dx;
                wpts[p++] = wy[i + 1];
            }
            hsv[0] = (hue + k * 22f) % 360f;
            hsv[1] = 0.65f;
            hsv[2] = 1f;
            line.setColor(Color.HSVToColor(hsv));
            line.setAlpha((int) (255 * (akk == 0 ? 0.95f : 0.55f - 0.1f * akk)));
            line.setStrokeWidth(den * ((akk == 0 ? 2.6f : 1.8f) + 3f * pulse + 1.5f * lvl));
            c.drawLines(wpts, 0, p, line);
        }
    }

    // =====================================================================

    @Override
    protected void onDraw(Canvas c) {
        if (W == 0 || H == 0 || builtFor != mode) {
            return;
        }
        switch (mode) {
            case COSMOS:
                drawCosmos(c);
                break;
            case FIREFLIES:
                drawFireflies(c);
                break;
            case WAVE:
                drawWave(c);
                break;
            default:
                break;
        }
    }
}
