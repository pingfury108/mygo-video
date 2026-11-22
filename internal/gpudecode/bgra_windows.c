// The picture ffmpeg decodes stays on the GPU as NV12, in a texture an array
// of slices; MyGo's renderer reads a BGRA texture shared by its NT handle,
// under a keyed mutex. This converts one to the other with a video processor,
// on the GPU as well: no pixel passes through memory.
#include <wchar.h>
#include <stdio.h>
#include <d3d11.h>
#include <d3d11_1.h>
#include <dxgi1_2.h>

// CONV_SLOTS is how many pictures may stand converted at once. The engine
// decodes a few pictures ahead of the sound, and each queued picture must
// keep its own texture: one texture reused for every decode shows the
// newest picture, not the one the engine chose for this moment.
#define CONV_SLOTS 8

typedef struct {
	ID3D11Texture2D                  *bgra;
	ID3D11VideoProcessorOutputView   *outView;
	IDXGIKeyedMutex                  *mutex;
	HANDLE                            handle;
} ConvOut;

typedef struct {
	ID3D11Device                     *dev;
	ID3D11DeviceContext              *ctx;
	ID3D11VideoDevice                *vdev;
	ID3D11VideoContext               *vctx;
	ID3D11VideoProcessorEnumerator   *vpe;
	ID3D11VideoProcessor             *vp;
	ConvOut                           out[CONV_SLOTS];
	ID3D11VideoProcessorInputView    *inViews[32];
	ID3D11Texture2D                  *inTex;
	UINT                              w, h;
} Conv;

// conv_init sets up the video processor that converts NV12 to BGRA. The BGRA
// textures are made a slot at a time, as the pictures that need them come.
static int conv_init(Conv *c, ID3D11Device *dev, ID3D11DeviceContext *ctx, UINT w, UINT h) {
	fprintf(stderr, "[conv] init dev=%p ctx=%p %ux%u\n", (void *)dev, (void *)ctx, w, h);
	c->dev = dev;
	c->ctx = ctx;
	if (FAILED(dev->lpVtbl->QueryInterface(dev, &IID_ID3D11VideoDevice, (void **)&c->vdev))) { fprintf(stderr, "[conv] no video device\n"); return -1; }
	fprintf(stderr, "[conv] video device ok\n");
	if (FAILED(ctx->lpVtbl->QueryInterface(ctx, &IID_ID3D11VideoContext, (void **)&c->vctx))) return -2;

	D3D11_VIDEO_PROCESSOR_CONTENT_DESC cd;
	ZeroMemory(&cd, sizeof(cd));
	cd.InputFrameFormat = D3D11_VIDEO_FRAME_FORMAT_PROGRESSIVE;
	cd.InputWidth = w;
	cd.InputHeight = h;
	cd.OutputWidth = w;
	cd.OutputHeight = h;
	cd.Usage = D3D11_VIDEO_USAGE_PLAYBACK_NORMAL;
	if (FAILED(c->vdev->lpVtbl->CreateVideoProcessorEnumerator(c->vdev, &cd, &c->vpe))) return -7;
	if (FAILED(c->vdev->lpVtbl->CreateVideoProcessor(c->vdev, c->vpe, 0, &c->vp))) return -8;

	// BT.709, the matrix HEVC nearly always carries.
	D3D11_VIDEO_PROCESSOR_COLOR_SPACE cs;
	ZeroMemory(&cs, sizeof(cs));
	cs.YCbCr_Matrix = 1;
	c->vctx->lpVtbl->VideoProcessorSetOutputColorSpace(c->vctx, c->vp, &cs);
	c->w = w;
	c->h = h;
	return 0;
}

static void conv_out_free(ConvOut *o) {
	if (o->outView) o->outView->lpVtbl->Release(o->outView);
	if (o->mutex) o->mutex->lpVtbl->Release(o->mutex);
	if (o->handle) CloseHandle(o->handle);
	if (o->bgra) o->bgra->lpVtbl->Release(o->bgra);
	ZeroMemory(o, sizeof(*o));
}

// conv_out_make makes the BGRA texture of a slot, shared by an NT handle with
// a keyed mutex, and the output view that writes it.
static int conv_out_make(Conv *c, int slot) {
	ConvOut *o = &c->out[slot];
	if (o->bgra) return 0;
	D3D11_TEXTURE2D_DESC td;
	ZeroMemory(&td, sizeof(td));
	td.Width = c->w;
	td.Height = c->h;
	td.MipLevels = 1;
	td.ArraySize = 1;
	td.Format = DXGI_FORMAT_B8G8R8A8_UNORM;
	td.SampleDesc.Count = 1;
	td.Usage = D3D11_USAGE_DEFAULT;
	td.BindFlags = D3D11_BIND_RENDER_TARGET | D3D11_BIND_SHADER_RESOURCE;
	td.MiscFlags = D3D11_RESOURCE_MISC_SHARED_KEYEDMUTEX | D3D11_RESOURCE_MISC_SHARED_NTHANDLE;
	if (FAILED(c->dev->lpVtbl->CreateTexture2D(c->dev, &td, NULL, &o->bgra))) return -3;

	IDXGIResource1 *res = NULL;
	if (FAILED(o->bgra->lpVtbl->QueryInterface(o->bgra, &IID_IDXGIResource1, (void **)&res))) {
		conv_out_free(o);
		return -4;
	}
	HRESULT hr = res->lpVtbl->CreateSharedHandle(res, NULL,
		DXGI_SHARED_RESOURCE_READ | DXGI_SHARED_RESOURCE_WRITE, NULL, &o->handle);
	res->lpVtbl->Release(res);
	if (FAILED(hr)) {
		conv_out_free(o);
		return -5;
	}
	if (FAILED(o->bgra->lpVtbl->QueryInterface(o->bgra, &IID_IDXGIKeyedMutex, (void **)&o->mutex))) {
		conv_out_free(o);
		return -6;
	}
	D3D11_VIDEO_PROCESSOR_OUTPUT_VIEW_DESC ovd;
	ZeroMemory(&ovd, sizeof(ovd));
	ovd.ViewDimension = D3D11_VPOV_DIMENSION_TEXTURE2D;
	if (FAILED(c->vdev->lpVtbl->CreateVideoProcessorOutputView(c->vdev,
		(ID3D11Resource *)o->bgra, c->vpe, &ovd, &o->outView))) {
		conv_out_free(o);
		return -7;
	}
	return 0;
}

// conv_blit converts slice index of the NV12 texture into the BGRA texture of
// a slot, under that slot's keyed mutex, and returns its handle. The slot
// belongs to the picture the engine queued, so a later decode cannot change
// what an earlier picture shows.
static int conv_blit(Conv *c, ID3D11Texture2D *nv12, int index, int slot, HANDLE *out) {
	if (slot < 0 || slot >= CONV_SLOTS) return -8;
	int rc = conv_out_make(c, slot);
	if (rc) return rc;
	ConvOut *o = &c->out[slot];
	// The input views are one per slice of the array the decoder keeps its
	// pictures in, which it reuses from frame to frame: made once, then kept.
	ID3D11VideoProcessorInputView *in = NULL;
	if (index >= 0 && index < 32 && c->inTex == nv12) {
		in = c->inViews[index];
	}
	if (!in) {
		D3D11_VIDEO_PROCESSOR_INPUT_VIEW_DESC ivd;
		ZeroMemory(&ivd, sizeof(ivd));
		ivd.ViewDimension = D3D11_VPIV_DIMENSION_TEXTURE2D;
		ivd.Texture2D.ArraySlice = (UINT)index;
		if (FAILED(c->vdev->lpVtbl->CreateVideoProcessorInputView(c->vdev,
			(ID3D11Resource *)nv12, c->vpe, &ivd, &in)))
			return -1;
		if (c->inTex != nv12) {
			for (int i = 0; i < 32; i++) {
				if (c->inViews[i]) {
					c->inViews[i]->lpVtbl->Release(c->inViews[i]);
					c->inViews[i] = NULL;
				}
			}
			c->inTex = nv12;
		}
		if (index >= 0 && index < 32) {
			c->inViews[index] = in;
		}
	}

	HRESULT hr = o->mutex->lpVtbl->AcquireSync(o->mutex, 0, 1000);
	if (hr != S_OK && hr != WAIT_ABANDONED) {
		return -3;
	}
	D3D11_VIDEO_PROCESSOR_STREAM st;
	ZeroMemory(&st, sizeof(st));
	st.Enable = TRUE;
	st.pInputSurface = in;
	hr = c->vctx->lpVtbl->VideoProcessorBlt(c->vctx, c->vp, o->outView, 0, 1, &st);
	o->mutex->lpVtbl->ReleaseSync(o->mutex, 0);
	if (FAILED(hr)) return -4;
	*out = o->handle;
	return 0;
}

static void conv_free(Conv *c) {
	for (int i = 0; i < 32; i++) {
		if (c->inViews[i]) c->inViews[i]->lpVtbl->Release(c->inViews[i]);
	}
	for (int i = 0; i < CONV_SLOTS; i++) conv_out_free(&c->out[i]);
	if (c->vp) c->vp->lpVtbl->Release(c->vp);
	if (c->vpe) c->vpe->lpVtbl->Release(c->vpe);
	if (c->vctx) c->vctx->lpVtbl->Release(c->vctx);
	if (c->vdev) c->vdev->lpVtbl->Release(c->vdev);
	ZeroMemory(c, sizeof(*c));
}
