#include <stdio.h>
#include <stdlib.h>

// The steps of opening, for the launch that crashes before a window shows:
// a GUI program has no stderr to read.
static FILE *declog(void) {
	static FILE *f = NULL;
	if (!f) {
		char p[512];
		snprintf(p, sizeof p, "%s\\mygo-dec.log", getenv("TEMP"));
		f = fopen(p, "a");
	}
	return f;
}
#define DLOG(...) do { FILE *f_ = declog(); if (f_) { fprintf(f_, __VA_ARGS__); fflush(f_); } } while (0)

#include <string.h>
#include <stdint.h>
#include <libavformat/avformat.h>
#include <libavcodec/avcodec.h>
#include <libavutil/hwcontext.h>
#include <libavutil/hwcontext_d3d11va.h>
#include <libavutil/pixfmt.h>
#include <libavformat/avio.h>
#include "bgra_windows.c"

// Dec is a decoder reading one video, its pictures on the GPU where the
// hardware takes them.
typedef struct {
	AVFormatContext *fmt;
	AVCodecContext  *dec;
	AVBufferRef     *hw;
	int              stream;
	AVPacket        *pkt;
	AVFrame         *frame;
	AVCodecParameters *par;
	void            *lastTex;   // the picture's texture, NV12
	int              lastIndex; // its slice in the array
	Conv             conv;      // NV12 to BGRA, on the GPU
	int              convReady;
} Dec;

// pick_d3d11 asks for Direct3D 11 pictures, and the system's own memory
// where the hardware refuses them.
static enum AVPixelFormat pick_d3d11(AVCodecContext *ctx, const enum AVPixelFormat *fmts) {

	for (const enum AVPixelFormat *p = fmts; *p != AV_PIX_FMT_NONE; p++) {
		if (*p == AV_PIX_FMT_D3D11) return *p;
	}
	return fmts[0];
}

static void dec_free(Dec *d) {
	conv_free(&d->conv);
	if (d->frame) av_frame_free(&d->frame);
	if (d->pkt) av_packet_free(&d->pkt);
	if (d->dec) avcodec_free_context(&d->dec);
	if (d->hw) av_buffer_unref(&d->hw);
	if (d->fmt) avformat_close_input(&d->fmt);
	memset(d, 0, sizeof(*d));
}

// dec_open reads the video at url, decoding with Direct3D 11 where it is.
// It returns 0, or a negative step number that failed.
static int dec_open(Dec *d, const char *url, const char *headers) {
	DLOG("dec_open: %s\n", url);
	int ni = avformat_network_init();
	DLOG("network_init=%d protocol=%s\n", ni, avio_find_protocol_name("http://x") ? avio_find_protocol_name("http://x") : "(null)");
	memset(d, 0, sizeof(*d));
	AVDictionary *opts = NULL;
	if (headers && headers[0]) av_dict_set(&opts, "headers", headers, 0);
	int r = avformat_open_input(&d->fmt, url, NULL, &opts);
	av_dict_free(&opts);
	if (r < 0) {
		char err[256];
		av_strerror(r, err, sizeof(err));
		fprintf(stderr, "[dec] open_input failed: %s (%d)\n", err, r);
		return -1;
	}
	DLOG("input opened, finding streams\n");
	if (avformat_find_stream_info(d->fmt, NULL) < 0) return -2;
	DLOG("streams found, video stream %d\n", d->stream);
	d->stream = av_find_best_stream(d->fmt, AVMEDIA_TYPE_VIDEO, -1, -1, NULL, 0);
	if (d->stream < 0) return -3;
	d->par = d->fmt->streams[d->stream]->codecpar;
	const AVCodec *codec = avcodec_find_decoder(d->par->codec_id);
	if (!codec) return -4;
	d->dec = avcodec_alloc_context3(codec);
	if (!d->dec) return -5;
	if (avcodec_parameters_to_context(d->dec, d->par) < 0) return -6;
	DLOG("making the d3d11 device\n");
	if (av_hwdevice_ctx_create(&d->hw, AV_HWDEVICE_TYPE_D3D11VA, NULL, NULL, 0) < 0) return -7;
	DLOG("d3d11 device %p, opening codec\n", (void *)d->hw);
	d->dec->hw_device_ctx = av_buffer_ref(d->hw);
	d->dec->get_format = pick_d3d11;
	if (avcodec_open2(d->dec, codec, NULL) < 0) return -8;
	DLOG("codec open, %dx%d\n", d->par->width, d->par->height);
	d->pkt = av_packet_alloc();
	d->frame = av_frame_alloc();
	if (!d->pkt || !d->frame) return -9;
	return 0;
}

// dec_next decodes the next picture. It returns 1 for a picture on the GPU,
// with its texture and index, 2 for one in memory, 0 at the end, and a
// negative step number that failed.
static int dec_next(Dec *d, void **tex, int *index, int64_t *pts, int *fmt) {
	for (;;) {
		int r = avcodec_receive_frame(d->dec, d->frame);
		if (r == 0) {
			*fmt = (int)d->frame->format;
			*pts = d->frame->pts;
			if (d->frame->format == AV_PIX_FMT_D3D11) {
				*tex = d->frame->data[0];
				*index = (int)(intptr_t)d->frame->data[1];
				d->lastTex = *tex;
				d->lastIndex = *index;
				return 1;
			}
			*tex = NULL;
			*index = -1;
			return 2;
		}
		if (r == AVERROR_EOF) return 0;
		if (r != AVERROR(EAGAIN)) return -20;
		if (av_read_frame(d->fmt, d->pkt) < 0) {
			avcodec_send_packet(d->dec, NULL);
			continue;
		}
		if (d->pkt->stream_index == d->stream) {
			avcodec_send_packet(d->dec, d->pkt);
		}
		av_packet_unref(d->pkt);
	}
}

static int dec_width(Dec *d)  { return d->par ? d->par->width : 0; }
static int dec_height(Dec *d) { return d->par ? d->par->height : 0; }

// dec_picture converts the picture last decoded into the BGRA texture the
// renderer reads, and returns its NT handle and the texture's size.
static int dec_picture(Dec *d, uintptr_t *handle, int *w, int *h) {
	if (!d->lastTex) return -1;
	if (!d->convReady) {
		AVHWDeviceContext *dc = (AVHWDeviceContext *)d->hw->data;
		AVD3D11VADeviceContext *hw = (AVD3D11VADeviceContext *)dc->hwctx;
		int rc = conv_init(&d->conv, hw->device, hw->device_context,
			(UINT)d->par->width, (UINT)d->par->height);
		if (rc) return rc;
		d->convReady = 1;
	}
	HANDLE hnd = NULL;
	int rc = conv_blit(&d->conv, (ID3D11Texture2D *)d->lastTex, d->lastIndex, &hnd);
	if (rc) return rc;
	*handle = (uintptr_t)hnd;
	*w = (int)d->conv.w;
	*h = (int)d->conv.h;
	return 0;
}

// dec_fps is the video's nominal rate, from what the container said.
static double dec_fps(Dec *d) {
	if (!d->fmt || d->stream < 0) return 25;
	AVRational r = d->fmt->streams[d->stream]->avg_frame_rate;
	if (r.den == 0) r = d->fmt->streams[d->stream]->r_frame_rate;
	if (r.den == 0) return 25;
	double f = (double)r.num / (double)r.den;
	return f > 0 ? f : 25;
}

// dec_duration_us is the video's length, in microseconds.
static int64_t dec_duration_us(Dec *d) {
	if (!d->fmt || d->fmt->duration == AV_NOPTS_VALUE) return 0;
	return d->fmt->duration;
}

// dec_seek_us goes to the picture at us, in microseconds.
static int dec_seek_us(Dec *d, int64_t us) {
	if (!d->fmt || d->stream < 0) return -1;
	AVStream *st = d->fmt->streams[d->stream];
	int64_t ts = av_rescale_q(us, AV_TIME_BASE_Q, st->time_base);
	int r = av_seek_frame(d->fmt, d->stream, ts, AVSEEK_FLAG_BACKWARD);
	if (r < 0) return r;
	if (d->dec) avcodec_flush_buffers(d->dec);
	d->lastTex = NULL;
	return 0;
}
