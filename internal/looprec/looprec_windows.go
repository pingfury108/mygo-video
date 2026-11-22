//go:build windows

// Package looprec records what the default output endpoint plays. It is how a
// check of the sync hears the sound: the engine's own clock says where the
// sound should be, and this says where it is.
package looprec

/*
#include <windows.h>
#include <mmdeviceapi.h>
#include <audioclient.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

// The IIDs below are the values Windows answers to. The mingw headers spell
// some of them differently, and an IID no device has fails silently.
static const GUID REC_CLSID_ENUM = {0xbcde0395,0xe52f,0x467c,{0x8e,0x3d,0xc4,0x57,0x92,0x91,0x69,0x2e}};
static const GUID REC_IID_ENUM = {0xa95664d2,0x9614,0x4f35,{0xa7,0x46,0xde,0x8d,0xb6,0x36,0x17,0xe6}};
static const GUID REC_IID_CLIENT = {0x1cb9ad4c,0xdbfa,0x4c32,{0xb1,0x78,0xc2,0xf5,0x68,0xa7,0x03,0xb2}};
static const GUID REC_IID_CAPTURE = {0xc8adbd64,0xe71e,0x48a0,{0xa4,0xde,0x18,0x5c,0x39,0x5c,0xd3,0x17}};

typedef struct {
	HANDLE    ready;
	HANDLE    stop;
	FILE     *out;
	int       rate;
	int       channels;
	int       bits;
	long long frames;
	// startedUnix is when the device began playing, in 100ns units since
	// 1601: taken in this thread the moment the stream started, where the
	// call that waits for it comes back to Go a while later.
	ULONGLONG started;
	char      err[256];
} Rec;

static void rec_err(Rec *r, const char *what, long hr) {
	snprintf(r->err, sizeof(r->err), "%s hr=%08lx", what, (unsigned long)hr);
}

DWORD WINAPI rec_thread(LPVOID arg) {
	Rec *r = (Rec *)arg;
	IMMDeviceEnumerator *enumr = NULL;
	IMMDevice *dev = NULL;
	IAudioClient *client = NULL;
	IAudioCaptureClient *cap = NULL;
	WAVEFORMATEX *fmt = NULL;

	CoInitializeEx(NULL, COINIT_MULTITHREADED);

	HRESULT hr = CoCreateInstance(&REC_CLSID_ENUM, NULL, CLSCTX_ALL,
	                              &REC_IID_ENUM, (void **)&enumr);
	if (FAILED(hr)) { rec_err(r, "enumerator", hr); goto done; }
	hr = enumr->lpVtbl->GetDefaultAudioEndpoint(enumr, eRender, eConsole, &dev);
	if (FAILED(hr)) { rec_err(r, "endpoint", hr); goto done; }
	hr = dev->lpVtbl->Activate(dev, &REC_IID_CLIENT, CLSCTX_ALL, NULL, (void **)&client);
	if (FAILED(hr)) { rec_err(r, "client", hr); goto done; }
	hr = client->lpVtbl->GetMixFormat(client, &fmt);
	if (FAILED(hr)) { rec_err(r, "mixformat", hr); goto done; }
	hr = client->lpVtbl->Initialize(client, AUDCLNT_SHAREMODE_SHARED,
	                                AUDCLNT_STREAMFLAGS_LOOPBACK, 2000000, 0, fmt, NULL);
	if (FAILED(hr)) { rec_err(r, "initialize", hr); goto done; }
	hr = client->lpVtbl->GetService(client, &REC_IID_CAPTURE, (void **)&cap);
	if (FAILED(hr)) { rec_err(r, "capture service", hr); goto done; }

	r->rate = fmt->nSamplesPerSec;
	r->channels = fmt->nChannels;
	r->bits = fmt->wBitsPerSample;
	if (r->bits != 32) { rec_err(r, "not float32", (long)r->bits); goto done; }

	hr = client->lpVtbl->Start(client);
	if (FAILED(hr)) { rec_err(r, "start", hr); goto done; }

	{
		FILETIME ft;
		GetSystemTimeAsFileTime(&ft);
		r->started = ((ULONGLONG)ft.dwHighDateTime << 32) | ft.dwLowDateTime;
	}
	SetEvent(r->ready);

	for (;;) {
		if (WaitForSingleObject(r->stop, 0) == WAIT_OBJECT_0) break;
		UINT32 packet = 0;
		hr = cap->lpVtbl->GetNextPacketSize(cap, &packet);
		if (FAILED(hr)) { rec_err(r, "packet size", hr); goto done; }
		if (packet == 0) {
			Sleep(2);
			continue;
		}
		BYTE *data = NULL;
		UINT32 frames = 0;
		DWORD flags = 0;
		hr = cap->lpVtbl->GetBuffer(cap, &data, &frames, &flags, NULL, NULL);
		if (FAILED(hr)) { rec_err(r, "get buffer", hr); goto done; }
		size_t one = (size_t)r->channels * (r->bits / 8);
		if (frames > 0) {
			if (flags & AUDCLNT_BUFFERFLAGS_SILENT) {
				// Silence takes its place too, or all that follows shifts.
				BYTE zero[1024];
				memset(zero, 0, sizeof(zero));
				for (UINT32 i = 0; i < frames; i++) fwrite(zero, 1, one, r->out);
			} else {
				fwrite(data, one, frames, r->out);
			}
			r->frames += frames;
		}
		cap->lpVtbl->ReleaseBuffer(cap, frames);
	}
	client->lpVtbl->Stop(client);

done:
	if (cap) cap->lpVtbl->Release(cap);
	if (client) client->lpVtbl->Release(client);
	if (fmt) CoTaskMemFree(fmt);
	if (dev) dev->lpVtbl->Release(dev);
	if (enumr) enumr->lpVtbl->Release(enumr);
	return 0;
}

// rec_close writes the WAVE header the samples made true, and closes the file.
static void rec_close(FILE *f, int rate, int channels, int bits, long long frames) {
	int block = channels * (bits / 8);
	DWORD dataSz = (DWORD)(frames * block);
	DWORD riffSz = dataSz + 36;
	unsigned char h[44];
	memcpy(h, "RIFF", 4);
	memcpy(h + 4, &riffSz, 4);
	memcpy(h + 8, "WAVEfmt ", 8);
	DWORD sz = 16;
	memcpy(h + 16, &sz, 4);
	WORD tag = 3; // IEEE float
	memcpy(h + 20, &tag, 2);
	WORD ch = (WORD)channels;
	memcpy(h + 22, &ch, 2);
	DWORD r = (DWORD)rate;
	memcpy(h + 24, &r, 4);
	DWORD br = (DWORD)(rate * block);
	memcpy(h + 28, &br, 4);
	WORD ba = (WORD)block;
	memcpy(h + 32, &ba, 2);
	WORD bps = (WORD)bits;
	memcpy(h + 34, &bps, 2);
	memcpy(h + 36, "data", 4);
	memcpy(h + 40, &dataSz, 4);
	fseek(f, 0, SEEK_SET);
	fwrite(h, 1, 44, f);
	fclose(f);
}
*/
import "C"

import (
	"errors"
	"fmt"
	"os"
	"time"
	"unsafe"
)

// Clip is what a recording holds.
type Clip struct {
	Path     string
	Rate     int
	Channels int
	Bits     int
	Started  time.Time
}

// Record writes what the default output endpoint plays into path for d.
func Record(path string, d time.Duration) (Clip, error) {
	cpath := C.CString(path)
	defer C.free(unsafe.Pointer(cpath))
	mode := C.CString("wb")
	defer C.free(unsafe.Pointer(mode))
	f := C.fopen(cpath, mode)
	if f == nil {
		return Clip{}, fmt.Errorf("looprec: cannot write %s", path)
	}

	r := (*C.Rec)(C.calloc(1, C.sizeof_Rec))
	if r == nil {
		C.fclose(f)
		return Clip{}, errors.New("looprec: no memory")
	}
	defer C.free(unsafe.Pointer(r))
	r.out = f
	r.ready = C.CreateEventW(nil, 0, 0, nil)
	r.stop = C.CreateEventW(nil, 0, 0, nil)
	defer C.CloseHandle(r.ready)
	defer C.CloseHandle(r.stop)

	thread := C.CreateThread(nil, 0, (C.LPTHREAD_START_ROUTINE)(C.rec_thread), C.LPVOID(unsafe.Pointer(r)), 0, nil)
	if thread == nil {
		C.fclose(f)
		return Clip{}, errors.New("looprec: no thread")
	}
	defer C.CloseHandle(thread)

	if C.WaitForSingleObject(r.ready, 5000) != C.WAIT_OBJECT_0 {
		C.SetEvent(r.stop)
		C.WaitForSingleObject(thread, 2000)
		C.fclose(f)
		return Clip{}, errors.New("looprec: device did not start")
	}
	started := time.Now()
	time.Sleep(d)
	C.SetEvent(r.stop)
	C.WaitForSingleObject(thread, 5000)

	if r.err[0] != 0 {
		C.fclose(f)
		return Clip{}, fmt.Errorf("looprec: %s", C.GoString(&r.err[0]))
	}
	// The moment the stream started, as the thread read it: 100ns units since
	// 1601, turned into the same clock the rest of the program times by.
	const epochDiff = 116444736000000000
	started = time.Unix(0, (int64(r.started)-epochDiff)*100).Local()
	rate, ch, bits, frames := int(r.rate), int(r.channels), int(r.bits), int64(r.frames)
	C.rec_close(f, C.int(rate), C.int(ch), C.int(bits), C.longlong(frames))
	return Clip{Path: path, Rate: rate, Channels: ch, Bits: bits, Started: started}, nil
}

// Temp returns a path under the temporary directory for a recording.
func Temp(name string) string {
	return os.TempDir() + string(os.PathSeparator) + name
}
