//go:build windows

package main

/*
#cgo LDFLAGS: -lgdi32
#include <windows.h>
#include <stdlib.h>

// grab_luma reads the average brightness of the desktop, 0..255. The picture
// of a check flashes white for a tenth of a second every second, and this is
// how its moment is taken on the same clock as the sound.
static int grab_luma(void) {
	HDC screen = GetDC(NULL);
	if (!screen) return -1;
	int w = GetSystemMetrics(SM_CXSCREEN);
	int h = GetSystemMetrics(SM_CYSCREEN);
	if (w <= 0 || h <= 0) { ReleaseDC(NULL, screen); return -1; }
	// A strip across the middle is enough to see a flash of the whole window.
	int y = h / 2, rows = 40;
	if (rows > h) rows = h;
	y -= rows / 2;
	if (y < 0) y = 0;

	HDC mem = CreateCompatibleDC(screen);
	HBITMAP bmp = CreateCompatibleBitmap(screen, w, rows);
	if (!mem || !bmp) {
		if (bmp) DeleteObject(bmp);
		if (mem) DeleteDC(mem);
		ReleaseDC(NULL, screen);
		return -1;
	}
	HGDIOBJ old = SelectObject(mem, bmp);
	BitBlt(mem, 0, 0, w, rows, screen, 0, y, SRCCOPY);

	BITMAPINFO bi;
	ZeroMemory(&bi, sizeof(bi));
	bi.bmiHeader.biSize = sizeof(BITMAPINFOHEADER);
	bi.bmiHeader.biWidth = w;
	bi.bmiHeader.biHeight = -rows; // top-down
	bi.bmiHeader.biPlanes = 1;
	bi.bmiHeader.biBitCount = 32;
	bi.bmiHeader.biCompression = BI_RGB;

	long long sum = 0;
	int count = 0;
	// A row at a time keeps the buffer small.
	unsigned char *line = (unsigned char *)malloc((size_t)w * 4);
	if (line) {
		for (int r = 0; r < rows; r++) {
			if (GetDIBits(mem, bmp, r, 1, line, &bi, DIB_RGB_COLORS)) {
				for (int x = 0; x < w; x++) {
					unsigned char *p = line + x * 4;
					sum += (p[0] + p[1] + p[2]) / 3;
					count++;
				}
			}
		}
		free(line);
	}

	SelectObject(mem, old);
	DeleteObject(bmp);
	DeleteDC(mem);
	ReleaseDC(NULL, screen);
	return count ? (int)(sum / count) : -1;
}
*/
import "C"

// luma is the brightness of the screen now, or -1.
func luma() int { return int(C.grab_luma()) }
