package com.local.sonidex;

import android.app.Activity;
import android.content.SharedPreferences;
import android.graphics.Typeface;
import android.os.Bundle;
import android.text.InputFilter;
import android.text.InputType;
import android.util.TypedValue;
import android.view.Gravity;
import android.view.View;
import android.widget.Button;
import android.widget.EditText;
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

    private EditText portEdit;
    private TextView status;
    private Button button;
    private Receiver receiver;

    private int dp(int v) {
        return Math.round(TypedValue.applyDimension(TypedValue.COMPLEX_UNIT_DIP, v,
                getResources().getDisplayMetrics()));
    }

    @Override
    protected void onCreate(Bundle state) {
        super.onCreate(state);
        setTitle("Sonidex Receiver");

        LinearLayout root = new LinearLayout(this);
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

        ScrollView scroll = new ScrollView(this);
        scroll.setFillViewport(true);
        scroll.addView(root);
        setContentView(scroll);
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
        receiver = self[0];
        status.setText("Listening for audio...");
        button.setText("Stop Receiving");
        receiver.start();
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
