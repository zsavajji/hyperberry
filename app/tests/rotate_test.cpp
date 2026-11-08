// Run on the Mac: clang++ -std=c++11 -I app/src app/tests/rotate_test.cpp -o /tmp/rotate_test && /tmp/rotate_test
#include "Nv12Rotate.h"

#include <assert.h>
#include <stdio.h>

// A 4x2 frame, rows padded to 6 bytes with 0xEE (which must never be copied), turned 0..3 times.
// The expected pictures are written out by hand, row by row.
int main()
{
    const unsigned char y[2 * 6] = { 0, 1, 2, 3, 0xEE, 0xEE,
                                     4, 5, 6, 7, 0xEE, 0xEE };
    // 2 pairs on 1 row: (10,11) and (12,13)
    const unsigned char uv[1 * 6] = { 10, 11, 12, 13, 0xEE, 0xEE };
    const unsigned char expY[4][4 * 4] = {
        { 0, 1, 2, 3,   4, 5, 6, 7 },
        { 4, 0,   5, 1,   6, 2,   7, 3 },
        { 7, 6, 5, 4,   3, 2, 1, 0 },
        { 3, 7,   2, 6,   1, 5,   0, 4 },
    };
    const unsigned char expUv[4][4] = {
        { 10, 11, 12, 13 },
        { 10, 11,   12, 13 },        // 1 wide, 2 tall: a then b
        { 12, 13, 10, 11 },
        { 12, 13,   10, 11 },        // b then a
    };
    for (int turns = 0; turns < 4; turns++) {
        const int dw = turns & 1 ? 2 : 4, dh = turns & 1 ? 4 : 2;       // luma
        const int cw = turns & 1 ? 1 : 2, ch = turns & 1 ? 2 : 1;       // chroma pairs
        unsigned char oy[5 * 8], ouv[5 * 8];
        memset(oy, 0xAA, sizeof oy);
        memset(ouv, 0xAA, sizeof ouv);
        rotateNv12(y, uv, 4, 2, 6, turns, false, oy, 5, ouv, 5);                // destination strides of 5
        for (int r = 0; r < dh; r++) {
            for (int c = 0; c < dw; c++)
                assert(oy[r * 5 + c] == expY[turns][r * dw + c]);
            assert(oy[r * 5 + dw] == 0xAA); // no write past the row
        }
        for (int r = 0; r < ch; r++) {
            for (int c = 0; c < cw * 2; c++)
                assert(ouv[r * 5 + c] == expUv[turns][r * cw * 2 + c]);
            assert(ouv[r * 5 + cw * 2] == 0xAA);
        }
    }
    // Mirrored, then turned once: the mirror is 3 2 1 0 / 7 6 5 4 with pairs b, a.
    {
        unsigned char oy[4 * 2], ouv[2 * 2];
        rotateNv12(y, uv, 4, 2, 6, 1, true, oy, 2, ouv, 2);
        const unsigned char wantY[8] = { 7, 3,   6, 2,   5, 1,   4, 0 };
        const unsigned char wantUv[4] = { 12, 13,   10, 11 };
        for (int i = 0; i < 8; i++)
            assert(oy[i] == wantY[i]);
        for (int i = 0; i < 4; i++)
            assert(ouv[i] == wantUv[i]);
        rotateNv12(y, uv, 4, 2, 6, 0, true, oy, 4, ouv, 4);
        const unsigned char flatY[8] = { 3, 2, 1, 0,   7, 6, 5, 4 };
        for (int i = 0; i < 8; i++)
            assert(oy[i] == flatY[i]);
        assert(ouv[0] == 12 && ouv[1] == 13 && ouv[2] == 10 && ouv[3] == 11);
    }
    // The crop a fill shows: a portrait picture in a square view, a landscape one, no view yet.
    int x, yy, cw, ch;
    centreCrop(212, 320, 720, 720, &x, &yy, &cw, &ch);
    assert(x == 0 && yy == 54 && cw == 212 && ch == 212);
    centreCrop(320, 180, 720, 720, &x, &yy, &cw, &ch);
    assert(x == 70 && yy == 0 && cw == 180 && ch == 180);
    centreCrop(320, 212, 0, 0, &x, &yy, &cw, &ch);
    assert(x == 0 && yy == 0 && cw == 320 && ch == 212);
    printf("rotate ok\n");
    return 0;
}
