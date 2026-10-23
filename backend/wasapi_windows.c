#define COBJMACROS
#define WIN32_LEAN_AND_MEAN
#include <windows.h>
#include <mmsystem.h>
#include <mmdeviceapi.h>
#include <audioclient.h>
#include <audiopolicy.h>
#include <stdlib.h>
#include <string.h>
#include "wasapi_windows.h"

/* GUIDs spelled out so no import library has to provide them. */
static const GUID SX_CLSID_MMDeviceEnumerator = {0xBCDE0395, 0xE52F, 0x467C, {0x8E, 0x3D, 0xC4, 0x57, 0x92, 0x91, 0x69, 0x2E}};
static const GUID SX_IID_IMMDeviceEnumerator = {0xA95664D2, 0x9614, 0x4F35, {0xA7, 0x46, 0xDE, 0x8D, 0xB6, 0x36, 0x17, 0xE6}};
static const GUID SX_IID_IAudioClient = {0x1CB9AD4C, 0xDBFA, 0x4C32, {0xB1, 0x78, 0xC2, 0xF5, 0x68, 0xA7, 0x03, 0xB2}};
static const GUID SX_IID_IAudioCaptureClient = {0xC8ADBD64, 0xE71E, 0x48A0, {0xA4, 0xDE, 0x18, 0x5C, 0x39, 0x5C, 0xD3, 0x17}};
static const GUID SX_IID_IAudioSessionManager2 = {0x77AA99A0, 0x1BD6, 0x484F, {0x8B, 0xC7, 0x2C, 0x65, 0x4C, 0x9A, 0x9B, 0x6F}};
static const GUID SX_IID_IAudioSessionControl2 = {0xBFB7FF88, 0x7239, 0x4FC9, {0x8F, 0xA2, 0x07, 0xC9, 0x50, 0xBE, 0x9C, 0x6D}};
static const GUID SX_IID_IAudioMeterInformation = {0xC02216F6, 0x8C67, 0x4B5B, {0x9D, 0x00, 0xD0, 0x08, 0xE7, 0x3E, 0x00, 0x64}};
static const GUID SX_IID_IAgileObject = {0x94EA2B94, 0xE9CC, 0x49E0, {0xC0, 0xFF, 0xEE, 0x64, 0xCA, 0x8F, 0x5B, 0x90}};
static const GUID SX_IID_ICompletionHandler = {0x41D949AB, 0x9862, 0x444A, {0x80, 0xF6, 0xC2, 0x61, 0x33, 0x4D, 0xA5, 0xEB}};

/* Minimal IAudioMeterInformation: the mingw header only declares it for C++. */
typedef struct SxMeter SxMeter;
typedef struct SxMeterVtbl {
	HRESULT(STDMETHODCALLTYPE *QueryInterface)(SxMeter *, REFIID, void **);
	ULONG(STDMETHODCALLTYPE *AddRef)(SxMeter *);
	ULONG(STDMETHODCALLTYPE *Release)(SxMeter *);
	HRESULT(STDMETHODCALLTYPE *GetPeakValue)(SxMeter *, float *);
} SxMeterVtbl;
struct SxMeter { const SxMeterVtbl *lpVtbl; };

/* Not in the mingw headers. */
HRESULT WINAPI ActivateAudioInterfaceAsync(LPCWSTR path, REFIID riid, PROPVARIANT *params,
                                           IActivateAudioInterfaceCompletionHandler *handler,
                                           IActivateAudioInterfaceAsyncOperation **op);

typedef struct {
	int ActivationType; /* 1 = AUDIOCLIENT_ACTIVATION_TYPE_PROCESS_LOOPBACK */
	DWORD TargetProcessId;
	int ProcessLoopbackMode; /* 0 = include tree, 1 = exclude tree */
} SxActivationParams;

/* ---- completion handler (agile) ---- */
typedef struct {
	IActivateAudioInterfaceCompletionHandler iface;
	LONG ref;
	HANDLE evt;
} SxHandler;

static HRESULT STDMETHODCALLTYPE h_qi(IActivateAudioInterfaceCompletionHandler *This, REFIID riid, void **out) {
	if (IsEqualIID(riid, &IID_IUnknown) || IsEqualIID(riid, &SX_IID_ICompletionHandler) ||
	    IsEqualIID(riid, &SX_IID_IAgileObject)) {
		*out = This;
		InterlockedIncrement(&((SxHandler *)This)->ref);
		return S_OK;
	}
	*out = NULL;
	return E_NOINTERFACE;
}
static ULONG STDMETHODCALLTYPE h_addref(IActivateAudioInterfaceCompletionHandler *This) {
	return InterlockedIncrement(&((SxHandler *)This)->ref);
}
static ULONG STDMETHODCALLTYPE h_release(IActivateAudioInterfaceCompletionHandler *This) {
	SxHandler *h = (SxHandler *)This;
	LONG r = InterlockedDecrement(&h->ref);
	if (r == 0) {
		CloseHandle(h->evt);
		free(h);
	}
	return r;
}
static HRESULT STDMETHODCALLTYPE h_done(IActivateAudioInterfaceCompletionHandler *This,
                                        IActivateAudioInterfaceAsyncOperation *op) {
	(void)op;
	SetEvent(((SxHandler *)This)->evt);
	return S_OK;
}
static IActivateAudioInterfaceCompletionHandlerVtbl g_hvtbl = {h_qi, h_addref, h_release, h_done};

static HRESULT activate_process_loopback(DWORD pid, int include, IAudioClient **out) {
	*out = NULL;
	SxActivationParams ap;
	memset(&ap, 0, sizeof ap);
	ap.ActivationType = 1;
	ap.TargetProcessId = pid;
	ap.ProcessLoopbackMode = include ? 0 : 1;

	PROPVARIANT pv;
	memset(&pv, 0, sizeof pv);
	pv.vt = VT_BLOB;
	pv.blob.cbSize = sizeof ap;
	pv.blob.pBlobData = (BYTE *)&ap;

	SxHandler *h = (SxHandler *)calloc(1, sizeof *h);
	if (!h) return E_OUTOFMEMORY;
	h->iface.lpVtbl = &g_hvtbl;
	h->ref = 1;
	h->evt = CreateEventW(NULL, FALSE, FALSE, NULL);

	IActivateAudioInterfaceAsyncOperation *op = NULL;
	HRESULT hr = ActivateAudioInterfaceAsync(L"VAD\\Process_Loopback", &SX_IID_IAudioClient, &pv,
	                                         &h->iface, &op);
	if (SUCCEEDED(hr)) {
		if (WaitForSingleObject(h->evt, 5000) != WAIT_OBJECT_0) {
			hr = HRESULT_FROM_WIN32(WAIT_TIMEOUT);
		} else {
			HRESULT act = E_FAIL;
			IUnknown *unk = NULL;
			hr = IActivateAudioInterfaceAsyncOperation_GetActivateResult(op, &act, &unk);
			if (SUCCEEDED(hr)) hr = act;
			if (SUCCEEDED(hr) && unk) {
				hr = IUnknown_QueryInterface(unk, &SX_IID_IAudioClient, (void **)out);
			}
			if (unk) IUnknown_Release(unk);
		}
	}
	if (op) IActivateAudioInterfaceAsyncOperation_Release(op);
	h_release(&h->iface);
	return hr;
}

/* ---- capture ---- */
struct SxCap {
	IAudioClient *client;
	IAudioCaptureClient *capture;
	HANDLE evt;
	int com_inited;
};

SxCap *sx_cap_open(unsigned long pid, int include, long *hr_out) {
	SxCap *c = (SxCap *)calloc(1, sizeof *c);
	if (!c) {
		if (hr_out) *hr_out = E_OUTOFMEMORY;
		return NULL;
	}
	HRESULT hr = CoInitializeEx(NULL, COINIT_MULTITHREADED);
	c->com_inited = SUCCEEDED(hr);

	hr = activate_process_loopback((DWORD)pid, include, &c->client);
	if (FAILED(hr)) goto fail;

	WAVEFORMATEX fmt;
	memset(&fmt, 0, sizeof fmt);
	fmt.wFormatTag = WAVE_FORMAT_PCM;
	fmt.nChannels = 2;
	fmt.nSamplesPerSec = 48000;
	fmt.wBitsPerSample = 16;
	fmt.nBlockAlign = 4;
	fmt.nAvgBytesPerSec = 48000 * 4;

	c->evt = CreateEventW(NULL, FALSE, FALSE, NULL);
	hr = IAudioClient_Initialize(c->client, AUDCLNT_SHAREMODE_SHARED,
	                             AUDCLNT_STREAMFLAGS_LOOPBACK | AUDCLNT_STREAMFLAGS_EVENTCALLBACK,
	                             400000 /* 40 ms */, 0, &fmt, NULL);
	if (FAILED(hr)) goto fail;
	hr = IAudioClient_SetEventHandle(c->client, c->evt);
	if (FAILED(hr)) goto fail;
	hr = IAudioClient_GetService(c->client, &SX_IID_IAudioCaptureClient, (void **)&c->capture);
	if (FAILED(hr)) goto fail;
	hr = IAudioClient_Start(c->client);
	if (FAILED(hr)) goto fail;
	if (hr_out) *hr_out = S_OK;
	return c;
fail:
	if (hr_out) *hr_out = hr;
	sx_cap_close(c);
	return NULL;
}

int sx_cap_read(SxCap *c, unsigned char *dst, int max_bytes, int timeout_ms) {
	DWORD w = WaitForSingleObject(c->evt, (DWORD)timeout_ms);
	if (w != WAIT_OBJECT_0 && w != WAIT_TIMEOUT) return -1;
	int total = 0;
	for (;;) {
		UINT32 pkt = 0;
		HRESULT hr = IAudioCaptureClient_GetNextPacketSize(c->capture, &pkt);
		if (FAILED(hr)) return -2;
		if (pkt == 0) break;
		BYTE *data = NULL;
		UINT32 frames = 0;
		DWORD flags = 0;
		hr = IAudioCaptureClient_GetBuffer(c->capture, &data, &frames, &flags, NULL, NULL);
		if (FAILED(hr)) return total > 0 ? total : -3;
		int bytes = (int)frames * 4;
		if (total + bytes > max_bytes) {
			IAudioCaptureClient_ReleaseBuffer(c->capture, 0); /* leave it for the next call */
			break;
		}
		if (flags & AUDCLNT_BUFFERFLAGS_SILENT) memset(dst + total, 0, bytes);
		else memcpy(dst + total, data, bytes);
		total += bytes;
		IAudioCaptureClient_ReleaseBuffer(c->capture, frames);
	}
	return total;
}

void sx_cap_close(SxCap *c) {
	if (!c) return;
	if (c->client) IAudioClient_Stop(c->client);
	if (c->capture) IAudioCaptureClient_Release(c->capture);
	if (c->client) IAudioClient_Release(c->client);
	if (c->evt) CloseHandle(c->evt);
	if (c->com_inited) CoUninitialize();
	free(c);
}

/* ---- session enumeration ---- */
static void exe_name(DWORD pid, char *out, size_t outsz) {
	out[0] = 0;
	HANDLE p = OpenProcess(PROCESS_QUERY_LIMITED_INFORMATION, FALSE, pid);
	if (!p) return;
	WCHAR path[MAX_PATH];
	DWORD n = MAX_PATH;
	if (QueryFullProcessImageNameW(p, 0, path, &n)) {
		WCHAR *base = path;
		for (WCHAR *q = path; *q; q++)
			if (*q == L'\\' || *q == L'/') base = q + 1;
		WideCharToMultiByte(CP_UTF8, 0, base, -1, out, (int)outsz, NULL, NULL);
	}
	CloseHandle(p);
}

int sx_list_sessions(SxSession *out, int max) {
	HRESULT hrc = CoInitializeEx(NULL, COINIT_MULTITHREADED);
	int inited = SUCCEEDED(hrc);
	int count = 0;
	IMMDeviceEnumerator *en = NULL;
	IMMDeviceCollection *coll = NULL;
	HRESULT hr = CoCreateInstance(&SX_CLSID_MMDeviceEnumerator, NULL, CLSCTX_ALL,
	                              &SX_IID_IMMDeviceEnumerator, (void **)&en);
	if (FAILED(hr)) { count = (int)hr; goto done; }
	hr = IMMDeviceEnumerator_EnumAudioEndpoints(en, eRender, DEVICE_STATE_ACTIVE, &coll);
	if (FAILED(hr)) { count = (int)hr; goto done; }
	UINT ndev = 0;
	IMMDeviceCollection_GetCount(coll, &ndev);
	DWORD self = GetCurrentProcessId();
	for (UINT d = 0; d < ndev; d++) {
		IMMDevice *dev = NULL;
		if (FAILED(IMMDeviceCollection_Item(coll, d, &dev))) continue;
		IAudioSessionManager2 *mgr = NULL;
		if (SUCCEEDED(IMMDevice_Activate(dev, &SX_IID_IAudioSessionManager2, CLSCTX_ALL, NULL, (void **)&mgr))) {
			IAudioSessionEnumerator *se = NULL;
			if (SUCCEEDED(IAudioSessionManager2_GetSessionEnumerator(mgr, &se))) {
				int ns = 0;
				IAudioSessionEnumerator_GetCount(se, &ns);
				for (int i = 0; i < ns; i++) {
					IAudioSessionControl *ctl = NULL;
					if (FAILED(IAudioSessionEnumerator_GetSession(se, i, &ctl))) continue;
					IAudioSessionControl2 *c2 = NULL;
					if (SUCCEEDED(IAudioSessionControl_QueryInterface(ctl, &SX_IID_IAudioSessionControl2, (void **)&c2))) {
						DWORD pid = 0;
						AudioSessionState st = AudioSessionStateInactive;
						IAudioSessionControl2_GetProcessId(c2, &pid);
						IAudioSessionControl_GetState(ctl, &st);
						if (pid != 0 && pid != self && IAudioSessionControl2_IsSystemSoundsSession(c2) != S_OK &&
						    st != AudioSessionStateExpired) {
							float peak = 0;
							SxMeter *mi = NULL;
							if (SUCCEEDED(IAudioSessionControl_QueryInterface(ctl, &SX_IID_IAudioMeterInformation, (void **)&mi))) {
								mi->lpVtbl->GetPeakValue(mi, &peak);
								mi->lpVtbl->Release(mi);
							}
							int idx = -1;
							for (int k = 0; k < count; k++)
								if (out[k].pid == pid) { idx = k; break; }
							if (idx < 0 && count < max) {
								idx = count++;
								memset(&out[idx], 0, sizeof out[idx]);
								out[idx].pid = pid;
								exe_name(pid, out[idx].name, sizeof out[idx].name);
							}
							if (idx >= 0) {
								if (st == AudioSessionStateActive) out[idx].active = 1;
								if (peak > out[idx].peak) out[idx].peak = peak;
							}
						}
						IAudioSessionControl2_Release(c2);
					}
					IAudioSessionControl_Release(ctl);
				}
				IAudioSessionEnumerator_Release(se);
			}
			IAudioSessionManager2_Release(mgr);
		}
		IMMDevice_Release(dev);
	}
done:
	if (coll) IMMDeviceCollection_Release(coll);
	if (en) IMMDeviceEnumerator_Release(en);
	if (inited) CoUninitialize();
	return count;
}

void sx_timer_begin(void) { timeBeginPeriod(1); }
void sx_timer_end(void) { timeEndPeriod(1); }
