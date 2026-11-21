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
#include <stdio.h>

// The device and client interfaces, whose identifiers mingw does not carry.
static const GUID IID_ENUM = {0xa95664d2,0x9614,0x4f35,{0xa7,0x46,0xde,0x8d,0xb6,0x36,0x17,0xe6}};
static const GUID IID_CLIENT = {0x1cb9ad4c,0xdbfa,0x4c32,{0xb1,0x78,0xc2,0xf5,0x68,0xa7,0x03,0xb2}};
static const GUID IID_RENDER = {0xf294acfc,0x3146,0x4483,{0xa7,0xbf,0xad,0xdc,0xa7,0xc2,0x60,0xe2}};
static const GUID IID_CLOCK = {0xcd63314f,0x3fba,0x4a1f,{0x81,0x2c,0xef,0x96,0x35,0x87,0x28,0xe7}};
static const GUID CLSID_ENUM = {0xbcde0395,0xe52f,0x467c,{0x8e,0x3d,0xc4,0x57,0x92,0x91,0x69,0x2e}};


typedef struct {
	AVFormatContext *fmt;
	AVCodecContext  *dec;
	SwrContext      *swr;
	int              stream;

	IAudioClient       *client;
	IAudioRenderClient *render;
	UINT64              written; // frames handed to the device
	UINT32              chunk; // frames a buffer holds
	int                 rate;  // samples a second the device takes
	int                 channels;
	int                 bytes; // a frame of all channels
	HANDLE              event;
	int                 started;
	int                 eof;    // the sound of the file is over
	int                 readErrs; // reads in a row that failed
	// startUs is the place the sound was sought to: the frames of earlier
	// places are dropped, as a seek lands before the one asked for.
	int64_t             startUs;
	AVRational          tb;     // the stream's time base, for those frames

	// pcm is what swr made of the audio frame last decoded, and left of it
	// how many frames are yet to play.
	uint8_t *pcm;
	int      pcmFrames;
	int      pcmAt;

	AVPacket *pkt;
	AVFrame  *frame;
} Audio;

static void audio_trace(Audio *a, const char *what, int n);

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

static void audio_apartment(void) {
	if (!t_apartment) {
		CoInitializeEx(NULL, COINIT_MULTITHREADED);
		t_apartment = 1;
	}
}

static int audio_open(Audio *a, const char *url, const char *headers, int64_t startUs,
                      int *outRate, int *outChannels) {
	memset(a, 0, sizeof(*a));
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
	a->channels = fmt->nChannels;
	a->bytes = fmt->nBlockAlign;
	if (outRate) *outRate = a->rate;
	if (outChannels) *outChannels = a->channels;

	enum AVSampleFormat outFmt = AV_SAMPLE_FMT_FLT;
	if (fmt->wFormatTag == WAVE_FORMAT_PCM || fmt->wBitsPerSample == 16) {
		outFmt = AV_SAMPLE_FMT_S16;
	}
	AVChannelLayout layout;
	av_channel_layout_default(&layout, a->channels);
	a->swr = swr_alloc();
	if (!a->swr) return -14;
	av_opt_set_chlayout(a->swr, "in_chlayout", &a->dec->ch_layout, 0);
	av_opt_set_int(a->swr, "in_sample_rate", a->dec->sample_rate, 0);
	av_opt_set_sample_fmt(a->swr, "in_sample_fmt", a->dec->sample_fmt, 0);
	av_opt_set_chlayout(a->swr, "out_chlayout", &layout, 0);
	av_opt_set_int(a->swr, "out_sample_rate", a->rate, 0);
	av_opt_set_sample_fmt(a->swr, "out_sample_fmt", outFmt, 0);
	av_channel_layout_uninit(&layout);
	if (swr_init(a->swr) < 0) return -15;

	REFERENCE_TIME dur = 10000000LL * 4 / 10; // 400ms of buffer
	audio_line("audio_open: initializing device");
	if (FAILED(a->client->lpVtbl->Initialize(a->client, AUDCLNT_SHAREMODE_SHARED,
		AUDCLNT_STREAMFLAGS_NOPERSIST, dur, 0, fmt, NULL))) {
		CoTaskMemFree(fmt);
		return -16;
	}
	CoTaskMemFree(fmt);
	if (FAILED(a->client->lpVtbl->GetBufferSize(a->client, &a->chunk))) return -17;
	if (FAILED(a->client->lpVtbl->GetService(a->client, &IID_RENDER,
		(void **)&a->render))) return -18;
	a->tb = a->fmt->streams[a->stream]->time_base;
	if (startUs > 0) audio_seek(a, startUs);
	audio_line("audio_open: device ready");
	return 0;
}

// audio_decode reads one more audio frame and resamples it; it returns how many
// frames are ready, or 0 at the end, or a negative step that failed.
static int audio_decode(Audio *a) {
	for (;;) {
		int r = avcodec_receive_frame(a->dec, a->frame);
		if (r == 0) {
			// A seek lands on the frame the index holds before the place
			// asked for, and the sound of the ones before it belongs to where
			// the playhead no longer is: playing it would run the sound ahead
			// of the picture by the length of what came first.
			if (a->startUs > 0 && a->frame->pts != AV_NOPTS_VALUE && a->tb.num > 0) {
				int64_t ptsUs = av_rescale_q(a->frame->pts, a->tb, AV_TIME_BASE_Q);
				if (ptsUs < a->startUs) {
					av_frame_unref(a->frame);
					continue;
				}
			}
			int cap = swr_get_out_samples(a->swr,
				a->frame->nb_samples + (a->frame->nb_samples % 100));
			if (cap <= 0) cap = a->frame->nb_samples * 2 + 256;
			int need = cap * a->channels * 4;
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
		memcpy(data + done * a->bytes,
			a->pcm + (size_t)a->pcmAt * a->bytes, (size_t)take * a->bytes);
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
		a->client->lpVtbl->Start(a->client);
		return 0;
	}
	// Fill the buffer before starting, so the sound does not crackle at once.
	audio_fill(a);
	HRESULT sr = a->client->lpVtbl->Start(a->client);
	audio_trace(a, "start", (int)sr);
	if (FAILED(sr)) return -26;
	a->started = 1;
	return 0;
}

static void audio_pause(Audio *a) {
	audio_apartment();
	if (a->started) a->client->lpVtbl->Stop(a->client);
}

// audio_position is how far the device has played, in microseconds: what was
// written, less what it still holds.
static int64_t audio_position(Audio *a) {
	audio_apartment();
	if (!a->started) return 0;
	UINT32 pad = 0;
	if (FAILED(a->client->lpVtbl->GetCurrentPadding(a->client, &pad))) return 0;
	UINT64 played = a->written > pad ? a->written - pad : 0;
	return (int64_t)(played * 1000000ULL / (UINT64)a->rate);
}

// audio_ended says the sound of the file is over, which the picture may
// outlast: a film whose closing titles run past its last sound is the usual
// case, and one where the picture would freeze at the last of it.
static int audio_ended(Audio *a) { return a->eof; }

static void audio_close(Audio *a) {
	audio_apartment();
	if (a->client) a->client->lpVtbl->Stop(a->client);
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
	a->pcmFrames = 0;
	a->pcmAt = 0;
	if (a->client) {
		a->client->lpVtbl->Stop(a->client);
		a->client->lpVtbl->Reset(a->client);
	}
	a->written = 0;
	a->started = 0;
	a->eof = 0;
	return 0;
}

// audio_trace writes what the device reports, for the sync that does not hold.
static void audio_trace(Audio *a, const char *what, int n) {
	FILE *f = fopen("C:\\Users\\superman\\AppData\\Local\\Temp\\mygo-audio-c.log", "a");
	if (!f) return;
	UINT32 pad = 0;
	if (a->client) a->client->lpVtbl->GetCurrentPadding(a->client, &pad);
	fprintf(f, "%s n=%d written=%llu pad=%u chunk=%u started=%d rate=%d\n",
		what, n, (unsigned long long)a->written, pad, a->chunk, a->started, a->rate);
	fclose(f);
}
