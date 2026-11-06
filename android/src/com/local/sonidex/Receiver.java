package com.local.sonidex;

import android.media.AudioAttributes;
import android.media.AudioFormat;
import android.media.AudioTrack;
import android.os.Build;

import java.io.IOException;
import java.io.InputStream;
import java.net.InetSocketAddress;
import java.net.ServerSocket;
import java.net.Socket;
import java.util.ArrayList;
import java.util.Arrays;
import java.util.List;

/**
 * Listens on a TCP port for raw 48 kHz stereo signed-16-bit PCM from the PC
 * streamer and plays it through a low-latency AudioTrack. Same behaviour as
 * the old Go receiver: a 250 ms drop-oldest ring buffer, 10 ms periods, and
 * the audio device paused after ~1 s of silence so the phone can idle.
 */
final class Receiver {
    interface Callback {
        /** May be called from any thread. */
        void status(String text);

        /** The receiver gave up on its own (not after stop()). */
        void failed();
    }

    private static final int RATE = 48000;
    private static final int PERIOD_BYTES = 1920; // 10 ms of 4-byte frames
    private static final int RING_BYTES = 48000; // 250 ms
    private static final int IDLE_PERIODS = 100; // ~1 s of silence
    private static final int MAX_RETRIES = 5;

    private final int port;
    private final Callback callback;
    private volatile boolean stopped;
    /** Optional: feeds the on-screen visualizer. Only called while it reports enabled. */
    private volatile Analyzer analyzer;

    private final Object lock = new Object();
    private final byte[] ring = new byte[RING_BYTES];
    private int tail; // read index
    private int size; // bytes buffered

    private ServerSocket server;
    private final List<Socket> conns = new ArrayList<Socket>();

    Receiver(int port, Callback callback) {
        this.port = port;
        this.callback = callback;
    }

    void setAnalyzer(Analyzer a) {
        analyzer = a;
    }

    void start() {
        new Thread(new Runnable() {
            public void run() {
                serve();
            }
        }, "sonidex-net").start();
        new Thread(new Runnable() {
            public void run() {
                play();
            }
        }, "sonidex-audio").start();
    }

    void stop() {
        stopped = true;
        synchronized (this) {
            closeQuietly(server);
            for (Socket s : conns) {
                closeQuietly(s);
            }
        }
        synchronized (lock) {
            lock.notifyAll();
        }
    }

    // ---- network ----

    private void serve() {
        long backoff = 500;
        for (int attempt = 1; !stopped; ) {
            try {
                listen();
                return; // only returns normally after stop()
            } catch (IOException e) {
                if (stopped) {
                    return;
                }
                if (attempt >= MAX_RETRIES) {
                    callback.status("Connection failed.");
                    callback.failed();
                    stop();
                    return;
                }
                callback.status("Disconnected. Reconnecting (" + attempt + "/" + MAX_RETRIES + ")...");
                attempt++;
                try {
                    Thread.sleep(backoff);
                } catch (InterruptedException ie) {
                    return;
                }
                backoff = Math.min(backoff * 2, 4000);
            }
        }
    }

    private void listen() throws IOException {
        ServerSocket ss = new ServerSocket();
        try {
            ss.setReuseAddress(true);
            ss.bind(new InetSocketAddress(port));
            synchronized (this) {
                if (stopped) {
                    return;
                }
                server = ss;
            }
            callback.status("Listening for audio...");
            while (!stopped) {
                final Socket s;
                try {
                    s = ss.accept();
                } catch (IOException e) {
                    if (stopped) {
                        return;
                    }
                    throw e;
                }
                synchronized (this) {
                    if (stopped) {
                        closeQuietly(s);
                        return;
                    }
                    conns.add(s);
                }
                new Thread(new Runnable() {
                    public void run() {
                        read(s);
                    }
                }, "sonidex-conn").start();
            }
        } finally {
            closeQuietly(ss);
        }
    }

    private void read(Socket s) {
        try {
            s.setTcpNoDelay(true);
            InputStream in = s.getInputStream();
            byte[] buf = new byte[8192];
            int rem = 0;
            while (!stopped) {
                int n = in.read(buf, rem, buf.length - rem);
                if (n < 0) {
                    break;
                }
                int total = rem + n;
                int aligned = total & ~3; // whole stereo S16 frames only
                if (aligned > 0) {
                    push(buf, aligned);
                }
                rem = total - aligned;
                if (rem > 0) {
                    System.arraycopy(buf, aligned, buf, 0, rem);
                }
            }
        } catch (IOException ignored) {
            // peer went away; keep listening for the next connection
        } finally {
            synchronized (this) {
                conns.remove(s);
            }
            closeQuietly(s);
        }
    }

    private static void closeQuietly(java.io.Closeable c) {
        if (c != null) {
            try {
                c.close();
            } catch (IOException ignored) {
            }
        }
    }

    // ---- ring buffer ----

    private void push(byte[] p, int n) {
        int off = 0;
        if (n > RING_BYTES) {
            off = n - RING_BYTES;
            n = RING_BYTES;
        }
        synchronized (lock) {
            int overflow = size + n - RING_BYTES;
            if (overflow > 0) { // drop the oldest audio to bound latency
                tail = (tail + overflow) % RING_BYTES;
                size -= overflow;
            }
            int head = (tail + size) % RING_BYTES;
            int first = Math.min(n, RING_BYTES - head);
            System.arraycopy(p, off, ring, head, first);
            if (n > first) {
                System.arraycopy(p, off + first, ring, 0, n - first);
            }
            size += n;
            lock.notifyAll();
        }
    }

    private int pop(byte[] out) {
        synchronized (lock) {
            int n = Math.min(out.length, size);
            if (n == 0) {
                return 0;
            }
            int first = Math.min(n, RING_BYTES - tail);
            System.arraycopy(ring, tail, out, 0, first);
            if (n > first) {
                System.arraycopy(ring, 0, out, first, n - first);
            }
            tail = (tail + n) % RING_BYTES;
            size -= n;
            return n;
        }
    }

    private void waitForData() {
        synchronized (lock) {
            while (size == 0 && !stopped) {
                try {
                    lock.wait();
                } catch (InterruptedException e) {
                    return;
                }
            }
        }
    }

    // ---- audio ----

    private AudioTrack newTrack() {
        int min = AudioTrack.getMinBufferSize(RATE, AudioFormat.CHANNEL_OUT_STEREO, AudioFormat.ENCODING_PCM_16BIT);
        AudioTrack.Builder b = new AudioTrack.Builder()
                .setAudioAttributes(new AudioAttributes.Builder()
                        .setUsage(AudioAttributes.USAGE_MEDIA)
                        .setContentType(AudioAttributes.CONTENT_TYPE_MUSIC)
                        .build())
                .setAudioFormat(new AudioFormat.Builder()
                        .setSampleRate(RATE)
                        .setChannelMask(AudioFormat.CHANNEL_OUT_STEREO)
                        .setEncoding(AudioFormat.ENCODING_PCM_16BIT)
                        .build())
                .setBufferSizeInBytes(Math.max(min, 3 * PERIOD_BYTES))
                .setTransferMode(AudioTrack.MODE_STREAM);
        if (Build.VERSION.SDK_INT >= 26) {
            b.setPerformanceMode(AudioTrack.PERFORMANCE_MODE_LOW_LATENCY);
        }
        return b.build();
    }

    private void play() {
        AudioTrack track;
        try {
            track = newTrack();
            track.play();
        } catch (RuntimeException e) {
            callback.status("Audio output unavailable.");
            callback.failed();
            stop();
            return;
        }
        byte[] chunk = new byte[PERIOD_BYTES];
        int empty = 0;
        while (!stopped) {
            int n = pop(chunk);
            if (n > 0) {
                Analyzer an = analyzer;
                if (an != null && an.enabled) { // a single volatile read when the visualizer is off/hidden
                    an.feed(chunk, n);
                }
            }
            if (n < PERIOD_BYTES) {
                Arrays.fill(chunk, n, PERIOD_BYTES, (byte) 0);
            }
            track.write(chunk, 0, PERIOD_BYTES); // blocks: paces the loop at 10 ms
            if (n > 0) {
                empty = 0;
            } else if (++empty == IDLE_PERIODS) {
                track.pause();
                track.flush();
                waitForData(); // sleep until the PC sends audio again
                if (stopped) {
                    break;
                }
                empty = 0;
                track.play();
            }
        }
        try {
            track.stop();
        } catch (IllegalStateException ignored) {
        }
        track.release();
    }
}
