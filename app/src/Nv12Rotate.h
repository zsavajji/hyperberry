#ifndef NV12ROTATE_H
#define NV12ROTATE_H

#include <stddef.h>
#include <string.h>

// Turning a linear NV12 picture upright (step 7c.3): the peer's frames come with the quarter turns
// that make them upright, and Screen's window draws a buffer as it is. Header-only and free of Qt so
// app/tests/rotate_test.cpp builds on the Mac.

// One plane of w x h elements of T (1 byte for luma, a 2-byte U,V pair for chroma) turned clockwise
// by `turns`. The destination is read off in order, row by row, because the window's buffer is
// write-only to the CPU and a scattered write is the slow one; the source is walked by steps that
// are set once per turn, so the inner loop has no branch.
template <typename T>
inline void rotatePlane(const unsigned char *src, int w, int h, int srcStride, int turns,
                        unsigned char *dst, int dstStride)
{
    const ptrdiff_t sz = sizeof(T), row = srcStride;
    const int dw = turns & 1 ? h : w, dh = turns & 1 ? w : h;
    const unsigned char *first;  // the source element that lands at the destination's top left
    ptrdiff_t perX, perY;        // source steps for one destination column, one destination row
    switch (turns & 3) {
    case 0: first = src;                            perX = sz;   perY = row; break;
    case 1: first = src + (h - 1) * row;            perX = -row; perY = sz;  break;
    case 2: first = src + (h - 1) * row + (w - 1) * sz; perX = -sz; perY = -row; break;
    default: first = src + (w - 1) * sz;            perX = row;  perY = -sz; break;
    }
    for (int y = 0; y < dh; y++) {
        const unsigned char *p = first + y * perY;
        unsigned char *out = dst + size_t(y) * dstStride;
        for (int x = 0; x < dw; x++, p += perX, out += sz)
            memcpy(out, p, sizeof(T)); // memcpy: no alignment or aliasing question for the compiler
    }
}

// The (U,V) pair as one 2-byte element, so the chroma turns as a half-size picture. (At namespace
// scope: the NDK's gcc takes no local type as a template argument.)
struct Nv12Pair { unsigned char u, v; };

// Linear NV12, w x h with even sides, rotated clockwise by quarterTurns (0 to 3; 0 copies). The
// output is h x w for an odd count. y and uv are the source planes (uv: w/2 pairs on h/2 rows), both
// with srcStride bytes per row; dy and duv the destination's, with their own strides.
inline void rotateNv12(const unsigned char *y, const unsigned char *uv, int w, int h, int srcStride,
                       int quarterTurns, unsigned char *dy, int dyStride, unsigned char *duv, int duvStride)
{
    rotatePlane<unsigned char>(y, w, h, srcStride, quarterTurns, dy, dyStride);
    rotatePlane<Nv12Pair>(uv, w / 2, h / 2, srcStride, quarterTurns, duv, duvStride);
}

#endif
