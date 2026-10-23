#ifndef SONIDEX_WASAPI_WINDOWS_H
#define SONIDEX_WASAPI_WINDOWS_H

typedef struct SxSession {
	unsigned long pid;
	char name[260 * 3]; /* UTF-8 exe base name */
	int active;         /* 1 = currently active session */
	float peak;         /* 0..1 meter peak */
} SxSession;

typedef struct SxCap SxCap;

/* Enumerates audio sessions on all active render endpoints, merged by PID.
 * Returns the count (>= 0) or a negative HRESULT. */
int sx_list_sessions(SxSession *out, int max);

/* Per-process loopback capture (Windows 10 2004+/11), 48 kHz S16 stereo.
 * include=1: only that process tree. include=0: everything except it.
 * Must be opened, read and closed from one OS thread (COM MTA). */
SxCap *sx_cap_open(unsigned long pid, int include, long *hr_out);
/* Waits up to timeout_ms for data. Returns bytes written, 0 on timeout,
 * negative on failure (device gone). */
int sx_cap_read(SxCap *c, unsigned char *dst, int max_bytes, int timeout_ms);
void sx_cap_close(SxCap *c);

void sx_timer_begin(void);
void sx_timer_end(void);

#endif
