// The sound of a video, decoded with libavcodec and played with WASAPI, the
// way the picture goes to the GPU: apart from the system's media framework,
// which the video itself does not use here.
#include <windows.h>
#include <mmdeviceapi.h>
#include <audioclient.h>
#include <libavformat/avformat.h>
#include <libavcodec/avcodec.h>
#include <libswresample/swresample.h>
#include <libavutil/opt.h>
#include <libavutil/time.h>
#include <math.h>
#include <stdio.h>

// The device and client interfaces, whose identifiers mingw does not carry.
static const GUID IID_ENUM = {0xa95664d2,0x9614,0x4f35,{0xa7,0x46,0xde,0x8d,0xb6,0x36,0x17,0xe6}};
static const GUID IID_CLIENT = {0x1cb9ad4c,0xdbfa,0x4c32,{0xb1,0x78,0xc2,0xf5,0x68,0xa7,0x03,0xb2}};
static const GUID IID_RENDER = {0xf294acfc,0x3146,0x4483,{0xa7,0xbf,0xad,0xdc,0xa7,0xc2,0x60,0xe2}};
// IID_CLOCK is the IID of IAudioClock as Windows answers it, which the mingw
// header this builds against spells differently: the value mpv asks with, the
// one a device answers (ao_wasapi_utils.c:56). Getting it wrong asks for an
// interface no device has, and the sound falls back to its buffer's padding,
// which misses what the mixer still holds.
static const GUID IID_CLOCK = {0xcd63314f,0x3fba,0x4a1b,{0x81,0x2c,0xef,0x96,0x35,0x87,0x28,0xe7}};
// IID_SIMPLE_VOLUME is ISimpleAudioVolume, whose identifier mingw leaves to
// uuid.lib, which this build does not link.
static const GUID IID_SIMPLE_VOLUME = {0x87ce5498,0x68d6,0x44e5,{0x92,0x15,0x6d,0xa4,0x7e,0xf8,0x83,0xd8}};
static const GUID CLSID_ENUM = {0xbcde0395,0xe52f,0x467c,{0x8e,0x3d,0xc4,0x57,0x92,0x91,0x69,0x2e}};
// The two subformats a shared-mode mix format names, whose identifiers
// come from ksmedia.h: the header is not carried here, and a PCM format
// read as float writes noise into the device.
static const GUID SUBTYPE_PCM = {0x00000001,0x0000,0x0010,{0x80,0x00,0x00,0xaa,0x00,0x38,0x9b,0x71}};
static const GUID SUBTYPE_FLOAT = {0x00000003,0x0000,0x0010,{0x80,0x00,0x00,0xaa,0x00,0x38,0x9b,0x71}};


typedef struct {
	AVFormatContext *fmt;
	AVCodecContext  *dec;
	SwrContext      *swr;
	int              stream;

	IAudioClient       *client;
	IAudioRenderClient *render;
	// volume is the session's own level, which the app's slider moves:
	// the device's mixer carries it, so a change is heard at once and the
	// samples written here stay what the decoder made.
	ISimpleAudioVolume *volume;
	// clock is the device's own playback position, which counts what the
	// mixer holds and the padding of the buffer does not.
	IAudioClock        *clock;
	UINT64              clockFreq;
	// posBase is what the clock read the moment the sound was sought, and
	// posBaseSet tells it was read: the clock counts from the device's own
	// start and a reset does not carry it back to zero on every driver, so
	// what the sound has played of the new place is what it read over that.
	int64_t             posBase;
	int                 posBaseSet;
	// probeLeft is how many reads after a seek the log takes, so the place
	// the clock names just after one shows: a seek that starts the count
	// somewhere else reads at once.
	int                 probeLeft;
	UINT64              written; // frames handed to the device
	UINT32              chunk; // frames a buffer holds
	int                 rate;  // samples a second the device takes
	// speed is how fast the film plays: the resampler makes the device's
	// rate carry that much more of the source, and the place read off the
	// device is put back on the film's timeline by it.
	double              speed;
	int                 channels;
	int                 bytes; // a frame of all channels
	int                 sampleBytes; // a sample swr made, which 24-bit PCM packs
	// outFmt is what swr writes into the device's buffer, from the mix
	// format the device answered with.
	enum AVSampleFormat outFmt;
	HANDLE              event;
	int                 noWait; // the first fill before the device runs waits for none
	int                 started;
	int                 eof;    // the sound of the file is over
	int                 readErrs; // reads in a row that failed
	// abort is the engine's close flag, shared with the picture's decoder:
	// a read that waits on the network is given up when the window closes.
	void               *abort;
	// startUs is the place the sound was sought to: the frames of earlier
	// places are dropped, as a seek lands before the one asked for.
	int64_t             startUs;
	AVRational          tb;     // the stream's time base, for those frames
	// firstUs is the timestamp of the first frame that reaches the device,
	// which a seek leaves short of the place it asked for: the sound's
	// samples count from there, and a playhead that starts them at the place
	// asked for runs ahead by the difference.
	int64_t             firstUs;
	int                 haveFirst;

	// pcm is what swr made of the audio frame last decoded, and left of it
	// how many frames are yet to play.
	uint8_t *pcm;
	int      pcmFrames;
	int      pcmAt;

	AVPacket *pkt;
	AVFrame  *frame;
} Audio;

static void audio_trace(Audio *a, const char *what, int n);
static int audio_swr_init(Audio *a);

// audio_out_format reads what the device's mix format holds. Shared mode
// answers with WAVE_FORMAT_EXTENSIBLE on most drivers, where the tag says
// nothing and the subformat says whether the bits are PCM or float; the
// valid bits are the ones to write, not the size of the container.
static int audio_out_format(Audio *a, const WAVEFORMATEX *fmt) {
	WORD tag = fmt->wFormatTag;
	WORD bits = fmt->wBitsPerSample;
	if (tag == WAVE_FORMAT_EXTENSIBLE && fmt->cbSize >= 22) {
		const WAVEFORMATEXTENSIBLE *ext = (const WAVEFORMATEXTENSIBLE *)fmt;
		if (IsEqualGUID(&ext->SubFormat, &SUBTYPE_FLOAT)) {
			tag = WAVE_FORMAT_IEEE_FLOAT;
		} else if (IsEqualGUID(&ext->SubFormat, &SUBTYPE_PCM)) {
			tag = WAVE_FORMAT_PCM;
		}
		if (ext->Samples.wValidBitsPerSample) {
			bits = ext->Samples.wValidBitsPerSample;
		}
	}
	switch (tag) {
	case WAVE_FORMAT_PCM:
		switch (bits) {
		case 8:  a->outFmt = AV_SAMPLE_FMT_U8;  break;
		case 16: a->outFmt = AV_SAMPLE_FMT_S16; break;
		case 24: // packed 3-byte samples, which swr makes as 32 and this packs
		case 32: a->outFmt = AV_SAMPLE_FMT_S32; break;
		default: return -15;
		}
		break;
	case WAVE_FORMAT_IEEE_FLOAT:
		switch (bits) {
		case 32: a->outFmt = AV_SAMPLE_FMT_FLT; break;
		case 64: a->outFmt = AV_SAMPLE_FMT_DBL; break;
		default: return -15;
		}
		break;
	default:
		// An unknown tag is the device's own; 16 bits is the one every
		// driver takes, and float is the default of shared mode.
		if (bits == 16) a->outFmt = AV_SAMPLE_FMT_S16;
		else a->outFmt = AV_SAMPLE_FMT_FLT;
		break;
	}
	a->sampleBytes = av_get_bytes_per_sample(a->outFmt);
	return 0;
}

static int audio_interrupted(Audio *a) {
	return a->abort && *(volatile LONG *)a->abort != 0;
}

static int audio_interrupt_cb(void *opaque) {
	return audio_interrupted((Audio *)opaque);
}

// audio_open reads the sound of the video at url, and asks the default output
// device for the format it takes.
// audio_seek is declared below, before audio_open runs it.
static int audio_seek(Audio *a, int64_t us);

// audio_co_init gives the calling thread a COM apartment. A device made on one
// thread and filled from another reaches WASAPI from a thread that has none,
// which hangs there rather than failing: the sound's own thread calls this.
static void audio_co_init(void) {
	CoInitializeEx(NULL, COINIT_MULTITHREADED);
}

// A goroutine moves between system threads, and every call into WASAPI must
// come from a thread with an apartment. This marks each thread the first time
// it makes one, so neither the loop that fills the device nor a seek from
// elsewhere reaches it from a thread that has none.
static __thread int t_apartment = 0;

// audio_line writes a line for the log of the sound, for the steps that come
// before the device exists and so have nothing to trace yet. The app has no
// console: the file is the only place it can be read.
static void audio_line(const char *msg) {
	const char *tmp = getenv("TEMP");
	if (!tmp) return;
	char path[512];
	snprintf(path, sizeof(path), "%s\\mygo-audio-c.log", tmp);
	FILE *f = fopen(path, "a");
	if (!f) return;
	fprintf(f, "%s\n", msg);
	fclose(f);
}

// audio_swr_init rebuilds the resampler, which a seek and a change of speed
// both need: the output rate is the device's rate divided by the speed, so
// the device plays a second of the film in 1/speed seconds.
static int audio_swr_init(Audio *a) {
	if (a->swr) swr_free(&a->swr);
	if (a->speed <= 0) a->speed = 1;
	int outRate = (int)lrint((double)a->rate / a->speed);
	if (outRate < 1000) outRate = 1000;
	AVChannelLayout layout;
	av_channel_layout_default(&layout, a->channels);
	a->swr = swr_alloc();
	if (!a->swr) {
		av_channel_layout_uninit(&layout);
		return -14;
	}
	av_opt_set_chlayout(a->swr, "in_chlayout", &a->dec->ch_layout, 0);
	av_opt_set_int(a->swr, "in_sample_rate", a->dec->sample_rate, 0);
	av_opt_set_sample_fmt(a->swr, "in_sample_fmt", a->dec->sample_fmt, 0);
	av_opt_set_chlayout(a->swr, "out_chlayout", &layout, 0);
	av_opt_set_int(a->swr, "out_sample_rate", outRate, 0);
	av_opt_set_sample_fmt(a->swr, "out_sample_fmt", a->outFmt, 0);
	av_channel_layout_uninit(&layout);
	if (swr_init(a->swr) < 0) {
		swr_free(&a->swr);
		return -15;
	}
	a->pcmFrames = 0;
	a->pcmAt = 0;
	return 0;
}

static void audio_apartment(void) {
	if (!t_apartment) {
		CoInitializeEx(NULL, COINIT_MULTITHREADED);
		t_apartment = 1;
	}
}

static int audio_open(Audio *a, const char *url, const char *headers, int64_t startUs,
                      double speed,
                      void *abort,
                      int *outRate, int *outChannels) {
	memset(a, 0, sizeof(*a));
	a->abort = abort;
	avformat_network_init();
	AVDictionary *opts = NULL;
	if (headers && headers[0]) av_dict_set(&opts, "headers", headers, 0);
	// A deeper read-ahead, and reconnects, for a source that stalls: the
	// default 32KB the avio buffer holds would put every hiccup of the
	// network straight onto the decoder.
	av_dict_set(&opts, "buffer_size", "524288", 0);
	av_dict_set(&opts, "reconnect_streamed", "1", 0);
	av_dict_set(&opts, "reconnect_delay_max", "4", 0);
	av_dict_set(&opts, "rw_timeout", "15000000", 0);
	audio_line("audio_open: open input");
	a->fmt = avformat_alloc_context();
	if (!a->fmt) {
		av_dict_free(&opts);
		return -1;
	}
	a->fmt->interrupt_callback.callback = audio_interrupt_cb;
	a->fmt->interrupt_callback.opaque = a;
	if (avformat_open_input(&a->fmt, url, NULL, &opts) < 0) return -1;
	av_dict_free(&opts);
	audio_line("audio_open: input opened");
	if (avformat_find_stream_info(a->fmt, NULL) < 0) return -2;
	a->stream = av_find_best_stream(a->fmt, AVMEDIA_TYPE_AUDIO, -1, -1, NULL, 0);
	if (a->stream < 0) return -3;
	if (avcodec_find_decoder(a->fmt->streams[a->stream]->codecpar->codec_id) == NULL) return -4;
	const AVCodec *codec = avcodec_find_decoder(a->fmt->streams[a->stream]->codecpar->codec_id);
	audio_line("audio_open: codec found");
	a->dec = avcodec_alloc_context3(codec);
	if (!a->dec) return -5;
	if (avcodec_parameters_to_context(a->dec, a->fmt->streams[a->stream]->codecpar) < 0) return -6;
	// The decoder hands a frame's timestamp back in this time base, as
	// mpv's decoder does (vd_lavc.c: avctx->pkt_timebase).
	a->dec->pkt_timebase = a->fmt->streams[a->stream]->time_base;
	if (avcodec_open2(a->dec, codec, NULL) < 0) return -7;
	a->pkt = av_packet_alloc();
	a->frame = av_frame_alloc();
	if (!a->pkt || !a->frame) return -8;

	HRESULT hr = CoInitializeEx(NULL, COINIT_MULTITHREADED);
	audio_line("audio_open: COM apartment");
	if (FAILED(hr)) return -900 - (int)(hr & 0xFF);
	IMMDeviceEnumerator *enumr = NULL;
	hr = CoCreateInstance(&CLSID_ENUM, NULL, CLSCTX_ALL, &IID_ENUM, (void **)&enumr);
	audio_line("audio_open: device enumerator");
	if (FAILED(hr)) return -1000 - (int)(hr & 0xFF);
	IMMDevice *dev = NULL;
	if (FAILED(enumr->lpVtbl->GetDefaultAudioEndpoint(enumr, eRender, eConsole, &dev))) {
		enumr->lpVtbl->Release(enumr);
		return -11;
	}
	enumr->lpVtbl->Release(enumr);
	// The device the sound comes out of, by name: a session sent over a
	// remote desktop plays it through the redirection of that session, which
	// buffers the sound on a way of its own, and nothing in the player can
	// know how long that way is.
	{
		LPWSTR id = NULL;
		if (SUCCEEDED(dev->lpVtbl->GetId(dev, &id)) && id) {
			char name[512];
			name[0] = 0;
			WideCharToMultiByte(CP_UTF8, 0, id, -1, name, sizeof(name) - 1, NULL, NULL);
			char line[600];
			snprintf(line, sizeof(line), "audio_open: device %s", name);
			audio_line(line);
			CoTaskMemFree(id);
		}
	}
	if (FAILED(dev->lpVtbl->Activate(dev, &IID_CLIENT, CLSCTX_ALL, NULL,
		(void **)&a->client))) {
		dev->lpVtbl->Release(dev);
		return -12;
	}
	dev->lpVtbl->Release(dev);
	audio_line("audio_open: client activated");
	WAVEFORMATEX *fmt = NULL;
	if (FAILED(a->client->lpVtbl->GetMixFormat(a->client, &fmt))) return -13;
	a->rate = fmt->nSamplesPerSec;
	a->speed = speed > 0 ? speed : 1;
	a->channels = fmt->nChannels;
	a->bytes = fmt->nBlockAlign;
	if (outRate) *outRate = a->rate;
	if (outChannels) *outChannels = a->channels;

	if (audio_out_format(a, fmt) < 0) {
		CoTaskMemFree(fmt);
		return -15;
	}
	if (audio_swr_init(a) < 0) {
		CoTaskMemFree(fmt);
		return -15;
	}

	REFERENCE_TIME dur = 10000000LL * 2 / 10; // 200ms of buffer, as mpv's
	// Read before the format is freed, which is where a wild value came
	// from: the rates on either side of the resampler, and the device format.
	{
		char buf[256];
		snprintf(buf, sizeof(buf),
			"audio_open: source %dHz %dch %s -> device %dHz %dch tag=0x%04x bits=%d block=%d chunk=%u",
			a->dec->sample_rate, a->dec->ch_layout.nb_channels,
			av_get_sample_fmt_name(a->dec->sample_fmt), a->rate, a->channels,
			(int)fmt->wFormatTag, (int)fmt->wBitsPerSample, (int)a->bytes, (unsigned)a->chunk);
		audio_line(buf);
	}
	audio_line("audio_open: initializing device");
	// Event driven, as mpv opens it: the device asks for the next period and
	// the sound is written then, so what it reports of its place and what it
	// plays stay one thing. Polled instead, the writes land wherever the
	// sleep let them, and the two come apart by however late that was.
	a->event = CreateEventW(NULL, FALSE, FALSE, NULL);
	DWORD flags = AUDCLNT_STREAMFLAGS_NOPERSIST;
	if (a->event) flags |= AUDCLNT_STREAMFLAGS_EVENTCALLBACK;
	if (FAILED(a->client->lpVtbl->Initialize(a->client, AUDCLNT_SHAREMODE_SHARED,
		flags, dur, 0, fmt, NULL))) {
		CoTaskMemFree(fmt);
		return -16;
	}
	CoTaskMemFree(fmt);
	if (a->event && FAILED(a->client->lpVtbl->SetEventHandle(a->client, a->event))) {
		CloseHandle(a->event);
		a->event = NULL;
	}
	audio_line(a->event ? "audio_open: event driven" : "audio_open: polled");
	if (FAILED(a->client->lpVtbl->GetBufferSize(a->client, &a->chunk))) return -17;
	if (FAILED(a->client->lpVtbl->GetService(a->client, &IID_RENDER,
		(void **)&a->render))) return -18;
	// The app's volume belongs to the session, not to the samples: the
	// device multiplies them on the way out, as a mixer does, so changing
	// it does not need the buffer rewritten.
	if (SUCCEEDED(a->client->lpVtbl->GetService(a->client, &IID_SIMPLE_VOLUME,
		(void **)&a->volume))) {
		audio_line("audio_open: volume service");
	} else {
		a->volume = NULL;
		audio_line("audio_open: no volume service");
	}
	// The device's clock counts the frames it has played, mixer and all: the
	// buffer's padding misses what the mixer still holds, and a playhead read
	// from it runs ahead of the sound by that much.
	if (SUCCEEDED(a->client->lpVtbl->GetService(a->client, &IID_CLOCK,
		(void **)&a->clock))) {
		UINT64 freq = 0;
		HRESULT fh = a->clock->lpVtbl->GetFrequency(a->clock, &freq);
		if (FAILED(fh))
			a->clockFreq = 0;
		else
			a->clockFreq = freq;
		char msg[160];
		snprintf(msg, sizeof(msg), "audio_open: clock freq=%llu getfreq hr=%08lx",
		         (unsigned long long)a->clockFreq, (unsigned long)fh);
		audio_line(msg);
	} else {
		audio_line("audio_open: no clock service");
	}
	a->tb = a->fmt->streams[a->stream]->time_base;
	// A seek at open that failed would leave the sound at the start while
	// the picture begins further on: the playhead would be right and the
	// sound wrong for the whole of the film, so the caller is told.
	if (startUs > 0 && audio_seek(a, startUs) != 0) return -19;
	audio_line("audio_open: device ready");
	return 0;
}

// audio_decode reads one more audio frame and resamples it; it returns how many
// frames are ready, or 0 at the end, or a negative step that failed.
static int audio_decode(Audio *a) {
	for (;;) {
		if (audio_interrupted(a)) return -99;
		int r = avcodec_receive_frame(a->dec, a->frame);
		if (r == 0) {
			// The frame's own place, where the container gave only a decode
			// timestamp: best_effort_timestamp estimates it, as mpv reads
			// a frame's pts.
			int64_t pts = a->frame->best_effort_timestamp;
			if (pts == AV_NOPTS_VALUE) pts = a->frame->pts;
			// A seek lands on the frame the index holds before the place
			// asked for, and the sound of the ones before it belongs to where
			// the playhead no longer is: playing it would run the sound ahead
			// of the picture by the length of what came first.
			if (a->startUs > 0 && pts != AV_NOPTS_VALUE && a->tb.num > 0) {
				int64_t ptsUs = av_rescale_q(pts, a->tb, AV_TIME_BASE_Q);
				if (ptsUs < a->startUs) {
					av_frame_unref(a->frame);
					continue;
				}
			}
			// The timestamp of the first sample the device will play: the
			// seek lands on the frame before the place it asked for, and the
			// sound of the frames it dropped does not exist, so this reaches
			// past the place asked for by however much it was rounded.
			if (!a->haveFirst && pts != AV_NOPTS_VALUE && a->tb.num > 0) {
				a->firstUs = av_rescale_q(pts, a->tb, AV_TIME_BASE_Q);
				a->haveFirst = 1;
				char buf[160];
				snprintf(buf, sizeof(buf), "audio_open: first frame %.3fs, sought %.3fs, %+dms",
					a->firstUs / 1e6, a->startUs / 1e6,
					(int)((a->firstUs - a->startUs) / 1000));
				audio_line(buf);
			}
			int cap = swr_get_out_samples(a->swr,
				a->frame->nb_samples + (a->frame->nb_samples % 100));
			if (cap <= 0) cap = a->frame->nb_samples * 2 + 256;
			int need = cap * a->channels * a->sampleBytes;
			if (!a->pcm || a->pcmFrames < need) {
				uint8_t *p = av_realloc(a->pcm, need);
				if (!p) return -21;
				a->pcm = p;
			}
			uint8_t *out[1] = { a->pcm };
			int n = swr_convert(a->swr, out, cap,
				(const uint8_t **)a->frame->extended_data, a->frame->nb_samples);
			if (n < 0) return -22;
			a->pcmFrames = n;
			a->pcmAt = 0;
			if (n > 0) return n;
			continue;
		}
		if (r == AVERROR_EOF) {
			a->eof = 1;
			return 0;
		}
		if (r != AVERROR(EAGAIN)) return -23;
		int rr = av_read_frame(a->fmt, a->pkt);
		if (rr < 0 && rr != AVERROR_EOF) {
			if (audio_interrupted(a)) return -99;
			// A read that failed partway is not the end of the sound: a
			// source that stalls is tried again, and only a run of failed
			// reads gives up, where the picture may then outlast it.
			if (++a->readErrs > 200) {
				a->eof = 1;
				return 0;
			}
			av_usleep(20000);
			return -100; // nothing this time, try again later
		}
		if (rr < 0) {
			avcodec_send_packet(a->dec, NULL);
			continue;
		}
		a->readErrs = 0;
		if (a->pkt->stream_index == a->stream) {
			avcodec_send_packet(a->dec, a->pkt);
		}
		av_packet_unref(a->pkt);
	}
}

// audio_fill writes what the device will play next into its buffer, and says
// how many frames it wrote; 0 when the sound is over.
static int audio_fill(Audio *a) {
	audio_apartment();
	// The device's own clock is waited on where it raises one: a period that
	// has room is written then, rather than whenever a sleep ended.
	if (a->event && !a->noWait) {
		DWORD w = WaitForSingleObject(a->event, 20);
		if (w == WAIT_TIMEOUT) return 0;
	}
	UINT32 pad = 0;
	if (FAILED(a->client->lpVtbl->GetCurrentPadding(a->client, &pad))) return -24;
	UINT32 want = a->chunk - pad;
	if (want == 0) return 0;
	BYTE *data = NULL;
	if (FAILED(a->render->lpVtbl->GetBuffer(a->render, want, &data))) return -25;
	UINT32 done = 0;
	while (done < want) {
		if (a->pcmAt >= a->pcmFrames) {
			int n = audio_decode(a);
			if (n == 0) break;
			if (n == -100) {
				// The source stalled: give back what was written and let the
				// loop come round again, rather than ending the sound.
				break;
			}
			if (n < 0) {
				a->render->lpVtbl->ReleaseBuffer(a->render, done, 0);
				return n;
			}
		}
		int avail = a->pcmFrames - a->pcmAt;
		int take = (int)(want - done);
		if (take > avail) take = avail;
		// A packed 24-bit PCM device holds three bytes of a sample that
		// swr made as four: the top three are the ones it plays, and the
		// low byte of the 32-bit sample is dropped.
		if (a->sampleBytes * a->channels == a->bytes) {
			memcpy(data + (size_t)done * a->bytes,
				a->pcm + (size_t)a->pcmAt * a->bytes,
				(size_t)take * a->bytes);
		} else if (a->sampleBytes == 4 && a->bytes == a->channels * 3) {
			const uint8_t *src = a->pcm + (size_t)a->pcmAt * a->sampleBytes * a->channels;
			uint8_t *dst = data + (size_t)done * a->bytes;
			for (int i = 0; i < take * a->channels; i++) {
				dst[i * 3 + 0] = src[i * 4 + 1];
				dst[i * 3 + 1] = src[i * 4 + 2];
				dst[i * 3 + 2] = src[i * 4 + 3];
			}
		} else {
			a->render->lpVtbl->ReleaseBuffer(a->render, done, 0);
			return -27;
		}
		a->pcmAt += take;
		done += take;
	}
	a->render->lpVtbl->ReleaseBuffer(a->render, done, 0);
	a->written += done;
	return (int)done;
}

// audio_play starts the device.
static int audio_play(Audio *a) {
	audio_apartment();
	if (a->started) {
		// Already running: starting it again is a call the sound's thread
		// makes every turn, and it is not the work of one of them.
		return 0;
	}
	// Fill the buffer before starting, so the sound does not crackle at once.
	// The device raises no period until it runs, so this first fill waits for
	// none.
	a->noWait = 1;
	audio_fill(a);
	a->noWait = 0;
	HRESULT sr = a->client->lpVtbl->Start(a->client);
	audio_trace(a, "start", (int)sr);
	if (FAILED(sr)) return -26;
	a->started = 1;
	return 0;
}

static void audio_pause(Audio *a) {
	audio_apartment();
	// Once: a stop carries the device's place back to zero, and one made
	// again and again while the sound waits for its picture keeps setting
	// the place it reports to zero — the playhead of a start then jumps
	// as the device settles, and the picture with it.
	if (a->started) {
		a->client->lpVtbl->Stop(a->client);
		a->started = 0;
	}
}

// audio_probe writes what the device clock says against what the software
// wrote, once in a while: an audio clock whose position includes samples the
// device has not played yet runs ahead of the sound, and every picture chosen
// off it runs ahead too. Read against the padding the device reports, it says
// which of the two the position is.
static void audio_probe(Audio *a, UINT64 position, UINT64 qpcPosition, int64_t us) {
	// Only the reads just after a seek are worth writing down: every read
	// of the device, once every few milliseconds, filled the log without
	// saying anything a listener would not see.
	if (a->probeLeft <= 0) return;
	a->probeLeft--;
	const char *tmp = getenv("TEMP");
	if (!tmp) return;
	char path[512];
	snprintf(path, sizeof(path), "%s\\mygo-audio-c.log", tmp);
	FILE *f = fopen(path, "a");
	if (!f) return;
	UINT32 pad = 0;
	if (a->client) a->client->lpVtbl->GetCurrentPadding(a->client, &pad);
	REFERENCE_TIME latency = 0;
	if (a->client) a->client->lpVtbl->GetStreamLatency(a->client, &latency);
	int64_t dev = (int64_t)((double)position * 1e6 / (double)a->clockFreq);
	int64_t written = (int64_t)(a->written * 1000000ULL / (UINT64)a->rate);
	int64_t padUs = (int64_t)(pad * 1000000ULL / (UINT64)a->rate);
	fprintf(f, "clock: devPos=%lldus base=%lldus written=%lldus pad=%lldus writtenLessPad=%lldus latency=%lldus report=%lldus\n",
	        (long long)dev, (long long)a->posBase, (long long)written, (long long)padUs,
	        (long long)(written - padUs), (long long)(latency / 10), (long long)us);
	fclose(f);
}

// audio_position is how far the device has played, in microseconds, read off
// its clock as mpv reads it (audio/out/ao_wasapi.c: get_device_delay): the
// frames the clock counts, less the time the call itself took.
static int64_t audio_position(Audio *a) {
	audio_apartment();
	if (!a->started) return 0;
	if (a->clock && a->clockFreq > 0) {
		UINT64 position = 0, qpcPosition = 0;
		if (SUCCEEDED(a->clock->lpVtbl->GetPosition(a->clock, &position, &qpcPosition))) {
			// A clock a reset carried back to zero starts the new place by
			// itself; one that kept counting has the place read over the
			// base. Whichever it is, the count of this place is what is left.
			int64_t played = (int64_t)position;
			if (a->posBaseSet) {
				int64_t over = played - a->posBase;
				if (over >= 0)
					played = over;
			}
			uint64_t frames = (uint64_t)((double)played * (double)a->rate / (double)a->clockFreq);
			int64_t us = (int64_t)(frames * 1000000ULL / (uint64_t)a->rate);
			// The clock reports where it was when it read, not where it is
			// now: the call's own length comes off (100ns units).
			LARGE_INTEGER qpc, freq;
			QueryPerformanceCounter(&qpc);
			QueryPerformanceFrequency(&freq);
			INT64 now = (INT64)((double)qpc.QuadPart * 1e7 / (double)freq.QuadPart);
			INT64 behind = now - (INT64)qpcPosition;
			// The clock read when it read, not now: the time since is the
			// device's own, and the place moves on by it.
			if (behind > 0 && behind < 10 * 10000000LL) us += behind / 10;
			// The device's own time is the film's divided by the speed:
			// put it back on the film's timeline before the place of the
			// first frame is added, which is already a film timestamp.
			if (a->speed != 1.0) us = (int64_t)((double)us * a->speed);
			if (us > 0) {
				// The samples count from the first frame that reached the
				// device, which a seek leaves past the place it asked for:
				// the shift puts the count on the stream's own timeline.
				us += a->haveFirst ? a->firstUs - a->startUs : 0;
			}
			audio_probe(a, position, qpcPosition, us);			return us > 0 ? us : 0;
		}
	}
	UINT32 pad = 0;
	if (FAILED(a->client->lpVtbl->GetCurrentPadding(a->client, &pad))) return 0;
	UINT64 played = a->written > pad ? a->written - pad : 0;
	return (int64_t)((double)played * 1000000.0 * a->speed / (double)a->rate);
}

// audio_delay is how long the sound held in the device will take to play, in
// microseconds: what was written and not yet played. mpv keeps a running
// account of it (ao->delay: audio written raises it, a picture shown lowers
// it); this reads the same thing off the device, one call, no drift.
static int64_t audio_delay_us(Audio *a) {
	audio_apartment();
	UINT32 pad = 0;
	// The padding is there whether or not the device runs: a sound filled
	// before it starts holds its frames, and reading that as none makes a
	// full buffer look like one that starved.
	if (FAILED(a->client->lpVtbl->GetCurrentPadding(a->client, &pad)))
		return 0;
	return (int64_t)pad * 1000000 / (UINT32)a->rate;
}

// audio_ended says the sound of the file is over, which the picture may
// outlast: a film whose closing titles run past its last sound is the usual
// case, and one where the picture would freeze at the last of it.
static int audio_ended(Audio *a) { return a->eof; }

static void audio_close(Audio *a) {
	audio_apartment();
	if (a->client) a->client->lpVtbl->Stop(a->client);
	if (a->volume) a->volume->lpVtbl->Release(a->volume);
	if (a->clock) a->clock->lpVtbl->Release(a->clock);
	if (a->render) a->render->lpVtbl->Release(a->render);
	if (a->client) a->client->lpVtbl->Release(a->client);
	if (a->event) CloseHandle(a->event);
	av_free(a->pcm);
	if (a->frame) av_frame_free(&a->frame);
	if (a->pkt) av_packet_free(&a->pkt);
	if (a->swr) swr_free(&a->swr);
	if (a->dec) avcodec_free_context(&a->dec);
	if (a->fmt) avformat_close_input(&a->fmt);
	memset(a, 0, sizeof(*a));
}

// audio_set_volume sets the session's level, from 0 to 1. It is called on
// the thread that owns the sound, as every other call into WASAPI is.
static int audio_set_volume(Audio *a, float v) {
	audio_apartment();
	if (!a->volume) return -40;
	if (v < 0) v = 0;
	if (v > 1) v = 1;
	if (FAILED(a->volume->lpVtbl->SetMasterVolume(a->volume, v, NULL)))
		return -41;
	return 0;
}

// audio_set_mute silences the session, or plays it again.
static int audio_set_mute(Audio *a, int m) {
	audio_apartment();
	if (!a->volume) return -40;
	if (FAILED(a->volume->lpVtbl->SetMute(a->volume, m ? TRUE : FALSE, NULL)))
		return -42;
	return 0;
}

// audio_seek goes to us in the sound, and empties what the device holds, so
// the sound follows the picture there rather than playing from the start.
static int audio_seek(Audio *a, int64_t us) {
	audio_apartment();
	if (!a->fmt || a->stream < 0) return -30;
	AVStream *st = a->fmt->streams[a->stream];
	a->tb = st->time_base;
	a->startUs = us;
	int64_t ts = av_rescale_q(us, AV_TIME_BASE_Q, st->time_base);
	if (av_seek_frame(a->fmt, a->stream, ts, AVSEEK_FLAG_BACKWARD) < 0) return -31;
	if (a->dec) avcodec_flush_buffers(a->dec);
	// The resampler holds what the frames before the seek left: rebuilding
	// it drops them, rather than mixing a few milliseconds of the place
	// left behind into the new one.
	if (audio_swr_init(a) < 0) return -32;
	a->haveFirst = 0;
	// Stop and empty the device before the clock is read: the place it stands
	// at then is the place the new sound starts counting from, and reading it
	// before the seek lets whatever the device played as the seek came in be
	// counted twice — a little more on every seek, and nothing lines up
	// after a hand has dragged the bar a few times.
	if (a->client) {
		a->client->lpVtbl->Stop(a->client);
		a->client->lpVtbl->Reset(a->client);
	}
	a->posBaseSet = 0;
	if (a->clock && a->clockFreq > 0) {
		UINT64 position = 0, qpcPosition = 0;
		if (SUCCEEDED(a->clock->lpVtbl->GetPosition(a->clock, &position, &qpcPosition))) {
			a->posBase = (int64_t)position;
			a->posBaseSet = 1;
		}
	}
	a->written = 0;
	a->started = 0;
	a->eof = 0;
	a->probeLeft = 4;
	char msg[160];
	snprintf(msg, sizeof(msg), "seek: asked=%lldus base=%lldus set=%d",
	         (long long)us, (long long)a->posBase, a->posBaseSet);
	audio_line(msg);
	return 0;
}

// audio_set_speed makes the film play speed times as fast. The output rate
// follows it, and the seek the engine sends next anchors the place the new
// rate counts from.
static int audio_set_speed(Audio *a, double speed) {
	audio_apartment();
	if (speed < 0.25) speed = 0.25;
	if (speed > 4) speed = 4;
	if (fabs(speed - a->speed) < 0.000001) return 0;
	a->speed = speed;
	return audio_swr_init(a);
}

// audio_trace writes what the device reports, for the sync that does not hold.
static void audio_trace(Audio *a, const char *what, int n) {
	const char *tmp = getenv("TEMP");
	if (!tmp) return;
	char path[512];
	snprintf(path, sizeof(path), "%s\\mygo-audio-c.log", tmp);
	FILE *f = fopen(path, "a");
	if (!f) return;
	UINT32 pad = 0;
	if (a->client) a->client->lpVtbl->GetCurrentPadding(a->client, &pad);
	fprintf(f, "%s n=%d written=%llu pad=%u chunk=%u started=%d rate=%d\n",
		what, n, (unsigned long long)a->written, pad, a->chunk, a->started, a->rate);
	fclose(f);
}
