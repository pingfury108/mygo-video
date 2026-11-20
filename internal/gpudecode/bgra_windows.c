// The picture ffmpeg decodes stays on the GPU as NV12, in a texture an array
// of slices; MyGo's renderer reads a BGRA texture shared by its NT handle,
// under a keyed mutex. This converts one to the other with a video processor,
// on the GPU as well: no pixel passes through memory.
#include <wchar.h>
#include <stdio.h>
#include <d3d11.h>
#include <d3d11_1.h>
#include <dxgi1_2.h>

typedef struct {
	ID3D11Device                     *dev;
	ID3D11DeviceContext              *ctx;
	ID3D11VideoDevice                *vdev;
	ID3D11VideoContext               *vctx;
	ID3D11VideoProcessorEnumerator   *vpe;
	ID3D11VideoProcessor             *vp;
	ID3D11Texture2D                  *bgra;
	ID3D11VideoProcessorOutputView   *outView;
	ID3D11VideoProcessorInputView    *inViews[32];
	ID3D11Texture2D                  *inTex;
	IDXGIKeyedMutex                  *mutex;
	HANDLE                            handle;
	UINT                              w, h;
} Conv;

// conv_init makes the BGRA texture the pictures land in, shares it by its NT
// handle, and sets up the video processor that converts NV12 to it.
static int conv_init(Conv *c, ID3D11Device *dev, ID3D11DeviceContext *ctx, UINT w, UINT h) {
	fprintf(stderr, "[conv] init dev=%p ctx=%p %ux%u\n", (void *)dev, (void *)ctx, w, h);
	c->dev = dev;
	c->ctx = ctx;
	if (FAILED(dev->lpVtbl->QueryInterface(dev, &IID_ID3D11VideoDevice, (void **)&c->vdev))) { fprintf(stderr, "[conv] no video device\n"); return -1; }
	fprintf(stderr, "[conv] video device ok\n");
	if (FAILED(ctx->lpVtbl->QueryInterface(ctx, &IID_ID3D11VideoContext, (void **)&c->vctx))) return -2;

	D3D11_TEXTURE2D_DESC td;
	ZeroMemory(&td, sizeof(td));
	td.Width = w;
	td.Height = h;
	td.MipLevels = 1;
	td.ArraySize = 1;
	td.Format = DXGI_FORMAT_B8G8R8A8_UNORM;
	td.SampleDesc.Count = 1;
	td.Usage = D3D11_USAGE_DEFAULT;
	td.BindFlags = D3D11_BIND_RENDER_TARGET | D3D11_BIND_SHADER_RESOURCE;
	td.MiscFlags = D3D11_RESOURCE_MISC_SHARED_KEYEDMUTEX | D3D11_RESOURCE_MISC_SHARED_NTHANDLE;
	if (FAILED(dev->lpVtbl->CreateTexture2D(dev, &td, NULL, &c->bgra))) { fprintf(stderr, "[conv] no texture\n"); return -3; }
	fprintf(stderr, "[conv] texture ok\n");

	IDXGIResource1 *res = NULL;
	if (FAILED(c->bgra->lpVtbl->QueryInterface(c->bgra, &IID_IDXGIResource1, (void **)&res))) return -4;
	HRESULT hr = res->lpVtbl->CreateSharedHandle(res, NULL,
		DXGI_SHARED_RESOURCE_READ | DXGI_SHARED_RESOURCE_WRITE, NULL, &c->handle);
	res->lpVtbl->Release(res);
	if (FAILED(hr)) return -5;

	if (FAILED(c->bgra->lpVtbl->QueryInterface(c->bgra, &IID_IDXGIKeyedMutex, (void **)&c->mutex))) return -6;

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

// conv_blit converts slice index of the NV12 texture into the BGRA one, under
// its keyed mutex, and returns the handle the renderer opens.
static int conv_blit(Conv *c, ID3D11Texture2D *nv12, int index, HANDLE *out) {
	// The output view never changes: every picture lands in the same texture.
	if (!c->outView) {
		D3D11_VIDEO_PROCESSOR_OUTPUT_VIEW_DESC ovd;
		ZeroMemory(&ovd, sizeof(ovd));
		ovd.ViewDimension = D3D11_VPOV_DIMENSION_TEXTURE2D;
		if (FAILED(c->vdev->lpVtbl->CreateVideoProcessorOutputView(c->vdev,
			(ID3D11Resource *)c->bgra, c->vpe, &ovd, &c->outView)))
			return -2;
	}
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

	HRESULT hr = c->mutex->lpVtbl->AcquireSync(c->mutex, 0, 1000);
	if (hr != S_OK && hr != WAIT_ABANDONED) {
		return -3;
	}
	D3D11_VIDEO_PROCESSOR_STREAM st;
	ZeroMemory(&st, sizeof(st));
	st.Enable = TRUE;
	st.pInputSurface = in;
	hr = c->vctx->lpVtbl->VideoProcessorBlt(c->vctx, c->vp, c->outView, 0, 1, &st);
	c->mutex->lpVtbl->ReleaseSync(c->mutex, 0);
	if (FAILED(hr)) return -4;
	*out = c->handle;
	return 0;
}

static void conv_free(Conv *c) {
	if (c->outView) c->outView->lpVtbl->Release(c->outView);
	for (int i = 0; i < 32; i++) {
		if (c->inViews[i]) c->inViews[i]->lpVtbl->Release(c->inViews[i]);
	}
	if (c->vp) c->vp->lpVtbl->Release(c->vp);
	if (c->vpe) c->vpe->lpVtbl->Release(c->vpe);
	if (c->mutex) c->mutex->lpVtbl->Release(c->mutex);
	if (c->handle) CloseHandle(c->handle);
	if (c->bgra) c->bgra->lpVtbl->Release(c->bgra);
	if (c->vctx) c->vctx->lpVtbl->Release(c->vctx);
	if (c->vdev) c->vdev->lpVtbl->Release(c->vdev);
	ZeroMemory(c, sizeof(*c));
}
