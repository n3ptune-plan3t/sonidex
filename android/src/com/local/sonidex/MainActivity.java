package com.local.sonidex;

import android.app.Activity;
import android.content.SharedPreferences;
import android.content.res.ColorStateList;
import android.graphics.Typeface;
import android.os.Bundle;
import android.text.InputFilter;
import android.text.InputType;
import android.util.TypedValue;
import android.view.Gravity;
import android.view.View;
import android.widget.Button;
import android.widget.EditText;
import android.widget.FrameLayout;
import android.widget.LinearLayout;
import android.widget.ScrollView;
import android.widget.TextView;

/**
 * The whole UI is stock android.widget views built in code: no AppCompat,
 * no Kotlin runtime, no layout inflation. Rendering and theming are the
 * platform's own.
 */
public class MainActivity extends Activity {
    private static final String PREFS = "sonidex";
    private static final String KEY_PORT = "port";
    private static final String KEY_VIS = "visualizer";
    private static final int ON_DARK = 0xFFE8ECF4;

    private EditText portEdit;
    private TextView status;
    private Button button;
    private Receiver receiver;

    // visualizer
    private VisualizerView vis;
    private Button visBtn;
    private TextView hint;
    private LinearLayout controls;
    private TextView[] tinted;
    private ColorStateList[] tintedOriginal;
    private int visMode;
    private boolean controlsHidden;

    private int dp(int v) {
        return Math.round(TypedValue.applyDimension(TypedValue.COMPLEX_UNIT_DIP, v,
                getResources().getDisplayMetrics()));
    }

    @Override
    protected void onCreate(Bundle state) {
        super.onCreate(state);
        setTitle("Sonidex Receiver");

        final LinearLayout root = new LinearLayout(this);
        controls = root;
        root.setOrientation(LinearLayout.VERTICAL);
        root.setPadding(dp(24), dp(24), dp(24), dp(24));

        TextView title = new TextView(this);
        title.setText("Sonidex Receiver");
        title.setTextSize(TypedValue.COMPLEX_UNIT_SP, 22);
        title.setTypeface(Typeface.DEFAULT_BOLD);
        title.setGravity(Gravity.CENTER_HORIZONTAL);
        root.addView(title, new LinearLayout.LayoutParams(
                LinearLayout.LayoutParams.MATCH_PARENT, LinearLayout.LayoutParams.WRAP_CONTENT));

        TextView portLabel = new TextView(this);
        portLabel.setText("Port:");
        LinearLayout.LayoutParams lp = new LinearLayout.LayoutParams(
                LinearLayout.LayoutParams.MATCH_PARENT, LinearLayout.LayoutParams.WRAP_CONTENT);
        lp.topMargin = dp(24);
        root.addView(portLabel, lp);

        portEdit = new EditText(this);
        portEdit.setInputType(InputType.TYPE_CLASS_NUMBER);
        portEdit.setSingleLine(true);
        portEdit.setFilters(new InputFilter[]{new InputFilter.LengthFilter(5)});
        portEdit.setText(getSharedPreferences(PREFS, MODE_PRIVATE).getString(KEY_PORT, "8080"));
        root.addView(portEdit, new LinearLayout.LayoutParams(
                LinearLayout.LayoutParams.MATCH_PARENT, LinearLayout.LayoutParams.WRAP_CONTENT));

        status = new TextView(this);
        status.setText("Ready");
        lp = new LinearLayout.LayoutParams(
                LinearLayout.LayoutParams.MATCH_PARENT, LinearLayout.LayoutParams.WRAP_CONTENT);
        lp.topMargin = dp(16);
        root.addView(status, lp);

        button = new Button(this);
        button.setText("Start Receiving");
        button.setOnClickListener(new View.OnClickListener() {
            public void onClick(View v) {
                toggle();
            }
        });
        lp = new LinearLayout.LayoutParams(
                LinearLayout.LayoutParams.MATCH_PARENT, LinearLayout.LayoutParams.WRAP_CONTENT);
        lp.topMargin = dp(8);
        root.addView(button, lp);

        visMode = getSharedPreferences(PREFS, MODE_PRIVATE).getInt(KEY_VIS, VisualizerView.OFF);
        if (visMode < 0 || visMode >= VisualizerView.MODES) {
            visMode = VisualizerView.OFF;
        }
        visBtn = new Button(this);
        visBtn.setOnClickListener(new View.OnClickListener() {
            public void onClick(View v) {
                visMode = (visMode + 1) % VisualizerView.MODES;
                getSharedPreferences(PREFS, MODE_PRIVATE).edit().putInt(KEY_VIS, visMode).apply();
                applyVisualizer();
            }
        });
        lp = new LinearLayout.LayoutParams(
                LinearLayout.LayoutParams.MATCH_PARENT, LinearLayout.LayoutParams.WRAP_CONTENT);
        lp.topMargin = dp(8);
        root.addView(visBtn, lp);

        hint = new TextView(this);
        hint.setText("Tap an empty area to hide or show these controls.");
        hint.setTextSize(TypedValue.COMPLEX_UNIT_SP, 12);
        lp = new LinearLayout.LayoutParams(
                LinearLayout.LayoutParams.MATCH_PARENT, LinearLayout.LayoutParams.WRAP_CONTENT);
        lp.topMargin = dp(8);
        root.addView(hint, lp);

        // Tapping the empty part of the screen hides the controls so the visual can be seen whole.
        root.setOnClickListener(new View.OnClickListener() {
            public void onClick(View v) {
                if (visMode != VisualizerView.OFF) {
                    setControlsHidden(!controlsHidden);
                }
            }
        });

        // Text that has to stay readable on the visualizer's dark backdrop.
        tinted = new TextView[]{title, portLabel, portEdit, status, hint};
        tintedOriginal = new ColorStateList[tinted.length];
        for (int i = 0; i < tinted.length; i++) {
            tintedOriginal[i] = tinted[i].getTextColors();
        }

        ScrollView scroll = new ScrollView(this);
        scroll.setFillViewport(true);
        scroll.addView(root);

        // The visualizer sits behind everything and covers the whole window.
        vis = new VisualizerView(this);
        FrameLayout frame = new FrameLayout(this);
        frame.addView(vis, new FrameLayout.LayoutParams(
                FrameLayout.LayoutParams.MATCH_PARENT, FrameLayout.LayoutParams.MATCH_PARENT));
        frame.addView(scroll, new FrameLayout.LayoutParams(
                FrameLayout.LayoutParams.MATCH_PARENT, FrameLayout.LayoutParams.MATCH_PARENT));
        setContentView(frame);
        applyVisualizer();
    }

    private void applyVisualizer() {
        final boolean on = visMode != VisualizerView.OFF;
        vis.setMode(visMode);
        visBtn.setText("Visualizer: " + VisualizerView.modeName(visMode));
        for (int i = 0; i < tinted.length; i++) {
            if (on) {
                tinted[i].setTextColor(ON_DARK);
            } else {
                tinted[i].setTextColor(tintedOriginal[i]);
            }
        }
        if (!on) {
            setControlsHidden(false);
        }
        hint.setVisibility(on && !controlsHidden ? View.VISIBLE : View.GONE);
    }

    @SuppressWarnings("deprecation") // legacy flags work on every API level this app supports
    private void setControlsHidden(boolean hide) {
        controlsHidden = hide;
        for (int i = 0; i < controls.getChildCount(); i++) {
            controls.getChildAt(i).setVisibility(hide ? View.INVISIBLE : View.VISIBLE);
        }
        hint.setVisibility(visMode != VisualizerView.OFF && !hide ? View.VISIBLE : View.GONE);
        getWindow().getDecorView().setSystemUiVisibility(hide
                ? View.SYSTEM_UI_FLAG_LAYOUT_STABLE | View.SYSTEM_UI_FLAG_LAYOUT_FULLSCREEN
                | View.SYSTEM_UI_FLAG_LAYOUT_HIDE_NAVIGATION | View.SYSTEM_UI_FLAG_FULLSCREEN
                | View.SYSTEM_UI_FLAG_HIDE_NAVIGATION | View.SYSTEM_UI_FLAG_IMMERSIVE_STICKY
                : View.SYSTEM_UI_FLAG_VISIBLE);
    }

    private void setStatus(final String text) {
        runOnUiThread(new Runnable() {
            public void run() {
                status.setText(text);
            }
        });
    }

    private void toggle() {
        if (receiver != null) {
            receiver.stop();
            receiver = null;
            status.setText("Receiver stopped.");
            button.setText("Start Receiving");
            return;
        }
        int port;
        try {
            port = Integer.parseInt(portEdit.getText().toString().trim());
        } catch (NumberFormatException e) {
            port = 8080;
        }
        if (port < 1 || port > 65535) {
            status.setText("Port must be 1-65535.");
            return;
        }
        getSharedPreferences(PREFS, MODE_PRIVATE).edit().putString(KEY_PORT, Integer.toString(port)).apply();

        final Receiver[] self = new Receiver[1];
        self[0] = new Receiver(port, new Receiver.Callback() {
            public void status(String text) {
                if (self[0] == receiver) {
                    setStatus(text);
                }
            }

            public void failed() {
                runOnUiThread(new Runnable() {
                    public void run() {
                        if (self[0] == receiver) {
                            receiver = null;
                            button.setText("Start Receiving");
                        }
                    }
                });
            }
        });
        self[0].setAnalyzer(vis.analyzer());
        receiver = self[0];
        status.setText("Listening for audio...");
        button.setText("Stop Receiving");
        receiver.start();
    }

    @Override
    protected void onResume() {
        super.onResume();
        vis.setResumed(true);
    }

    @Override
    protected void onPause() {
        // Screen off, home, another app in front, a dialog... all land here: stop drawing and analysing.
        vis.setResumed(false);
        super.onPause();
    }

    @Override
    protected void onDestroy() {
        if (receiver != null) {
            receiver.stop();
            receiver = null;
        }
        super.onDestroy();
    }
}
